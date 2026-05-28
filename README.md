# demo-app — example CI/CD pipeline for Kubernetes

A minimal Go HTTP service with everything you need to demo a real
build → image → deploy pipeline against a Kubernetes cluster:

- **App**: ~100 lines of Go (`/`, `/health`, `/version`).
- **Container**: multi-stage Dockerfile, distroless runtime, non-root, ~10 MB.
- **Kubernetes**: namespace, ConfigMap, Deployment (probes, resources,
  rolling update, topology spread, hardened securityContext), ClusterIP Service.
- **CI/CD**: GitHub Actions workflow that tests, builds, pushes to GHCR,
  then `kubectl set image` to roll out and `kubectl rollout status` to
  wait. Includes an in-cluster smoke test.

```
demo-app/
├── app/
│   ├── main.go
│   └── go.mod
├── Dockerfile
├── .dockerignore
├── k8s/
│   ├── namespace.yaml
│   ├── configmap.yaml
│   ├── deployment.yaml
│   └── service.yaml
├── .github/workflows/
│   └── ci-cd.yml
└── README.md
```

---

## 1. Run locally (sanity check)

```bash
cd app
go run .
# in another shell:
curl localhost:8080/
curl localhost:8080/health
curl localhost:8080/version
```

Build the container locally:

```bash
docker build -t demo-app:dev --build-arg VERSION=local --build-arg COMMIT=$(git rev-parse --short HEAD) .
docker run --rm -p 8080:8080 demo-app:dev
```

---

## 2. Deploy manually (first time)

Replace `REPLACE_ME` in `k8s/deployment.yaml` with your GitHub
org/user, e.g. `ghcr.io/yourname/demo-app:latest`. Then:

```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/deployment.yaml

kubectl -n demo-app rollout status deploy/demo-app
kubectl -n demo-app get pods -o wide
```

Reach the service (ClusterIP, so use port-forward):

```bash
kubectl -n demo-app port-forward svc/demo-app 8080:80
# then:
curl localhost:8080/
curl localhost:8080/health
```

---

## 3. One-time setup for the GitHub Actions pipeline

The pipeline pushes images to **GitHub Container Registry (GHCR)** and
deploys with **kubectl**. You need three things:

### a. Push the repo to GitHub

```bash
git init && git add . && git commit -m "init"
git remote add origin git@github.com:<you>/<repo>.git
git push -u origin main
```

The workflow triggers on pushes to `main`.

### b. Add a `KUBE_CONFIG` secret

Base64-encode a kubeconfig that has permission to update the
`demo-app` Deployment, and add it as a repo secret:

```bash
# On the machine that already talks to the cluster (e.g. your jump host):
base64 -w0 ~/.kube/config        # Linux
base64 -i ~/.kube/config         # macOS
```

In GitHub: **Settings → Secrets and variables → Actions → New repository secret**

- Name: `KUBE_CONFIG`
- Value: the base64 string

For production you'd use a dedicated ServiceAccount with a narrow Role
(see "Tighten RBAC" below), not your personal kubeconfig.

### c. Create the GHCR image-pull Secret in the cluster

GHCR images are private by default, so the cluster needs creds to pull
them. Create a Personal Access Token with `read:packages` scope, then:

```bash
kubectl -n demo-app create secret docker-registry ghcr-pull \
  --docker-server=ghcr.io \
  --docker-username=<github-username> \
  --docker-password=<PAT-with-read:packages> \
  [email protected]
```

The Deployment already references this secret via `imagePullSecrets:
[{name: ghcr-pull}]`.

(If you make the package public in GHCR — Packages → demo-app → Package
settings → Change visibility — you can skip the pull secret entirely.)

---

## 4. Trigger the pipeline

```bash
# Make any change in app/ and push:
echo "// touched" >> app/main.go
git commit -am "trigger ci" && git push
```

In the **Actions** tab on GitHub you'll see three jobs run in sequence:

1. **Test** — `go vet` + `go test -race`.
2. **Build & push image** — builds with Buildx, tags as
   `ghcr.io/<repo>/demo-app:sha-<commit>` and `:latest`, pushes to GHCR.
3. **Deploy to Kubernetes** —
   - applies namespace/configmap/service (idempotent);
   - on first run, also applies the Deployment;
   - on subsequent runs, runs `kubectl set image` with the new image
     **digest** (pinned, not a moving tag) and waits for rollout;
   - runs an in-cluster `curl /health` smoke test.

---

## 5. Verify after deploy

```bash
kubectl -n demo-app get pods
kubectl -n demo-app logs -l app.kubernetes.io/name=demo-app --tail=50
kubectl -n demo-app rollout history deploy/demo-app

# Reach the new version:
kubectl -n demo-app port-forward svc/demo-app 8080:80
curl localhost:8080/version
```

To roll back to the previous revision:

```bash
kubectl -n demo-app rollout undo deploy/demo-app
```

---

## 6. Tighten RBAC (production-grade)

The pipeline only needs to `get`/`patch` one Deployment and `create`
the smoke-test Pod. Create a dedicated ServiceAccount with a narrow
Role, and put **its** kubeconfig into `KUBE_CONFIG`:

```yaml
# k8s/ci-rbac.yaml  (example, not applied by default)
apiVersion: v1
kind: ServiceAccount
metadata: { name: ci, namespace: demo-app }
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: { name: ci-deployer, namespace: demo-app }
rules:
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch", "patch", "update"]
- apiGroups: [""]
  resources: ["pods", "pods/log", "configmaps", "services"]
  verbs: ["get", "list", "watch", "create", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: { name: ci-deployer, namespace: demo-app }
subjects:
- kind: ServiceAccount
  name: ci
  namespace: demo-app
roleRef:
  kind: Role
  name: ci-deployer
  apiGroup: rbac.authorization.k8s.io
```

Then mint a short-lived token for the workflow:

```bash
TOKEN=$(kubectl -n demo-app create token ci --duration=24h)
# Build a kubeconfig that uses it, base64 it, store as KUBE_CONFIG.
```

---

## 7. What you'd add for real production

- **Horizontal Pod Autoscaler** on CPU/RPS.
- **PodDisruptionBudget** so node drains don't kill all replicas.
- **Image signing** (cosign) + verification (policy-controller).
- **SCA / vuln scanning** in CI (e.g. Trivy step) — failing builds on
  CRITICAL CVEs.
- **Environments**: `staging` → `production` with a manual approval
  gate (GitHub Environments + `environment:` in the workflow already
  set up for this).
- **GitOps**: replace the deploy job with a commit that bumps a tag in
  a separate manifests repo watched by Argo CD or Flux.
