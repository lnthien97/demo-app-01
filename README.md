# nginx-app — static site served by nginx, with CI/CD

A minimal nginx-based service to demo the same CI/CD pipeline as `../demo-app/`
without writing application code. The pipeline builds a custom image
(static site + nginx.conf baked in), pushes to GHCR, and rolls it out with
`kubectl set image`.

```
nginx-app/
├── site/
│   ├── index.html              landing page (shows version/commit/built from /version.json)
│   └── version.json.tmpl       placeholders filled in by the Dockerfile
├── nginx.conf                  custom config: listens on :8080, /health endpoint, logs to stdout
├── Dockerfile                  nginxinc/nginx-unprivileged base; non-root; injects build info
├── .dockerignore
├── k8s/
│   ├── namespace.yaml          nginx-app namespace
│   ├── deployment.yaml         2 replicas, probes, readOnlyRootFilesystem + tmp volumes
│   └── service.yaml            ClusterIP :80 -> pod :8080
├── .github/workflows/ci-cd.yml lint → build & push → deploy → smoke test
└── README.md
```

> Why a custom image instead of the upstream `nginx:alpine` + a ConfigMap?
> Both work. Baking content into an immutable image is the CI/CD-friendly
> path — each commit produces an image with a unique digest, and rollouts
> and rollbacks are just `kubectl set image` to a different digest. Using a
> ConfigMap means updating content doesn't trigger a rollout, which is
> sometimes what you want and sometimes not. We pick the image path here so
> there's something to build.

---

## 1. Run locally

```bash
docker build -t nginx-app:dev \
  --build-arg VERSION=local \
  --build-arg COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo none) .

docker run --rm -p 8080:8080 nginx-app:dev
# then:
curl localhost:8080/health
curl localhost:8080/version.json
open  http://localhost:8080/         # or just visit in a browser
```

---

## 2. Deploy manually (first time)

Replace `REPLACE_ME` in `k8s/deployment.yaml` with your GitHub org/user
(e.g. `ghcr.io/yourname/nginx-app:latest`).  Then:

```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/service.yaml
kubectl apply -f k8s/deployment.yaml

kubectl -n nginx-app rollout status deploy/nginx-app
kubectl -n nginx-app get pods
```

Reach the service (ClusterIP, so port-forward):

```bash
kubectl -n nginx-app port-forward svc/nginx-app 8080:80
curl localhost:8080/health
curl localhost:8080/version.json
```

---

## 3. One-time GitHub setup

Identical to `../demo-app/` — see that README for full details. The short
version:

1. Push this repo to GitHub.
2. Add a repo secret `KUBE_CONFIG` = base64 of a kubeconfig that can patch
   the `nginx-app` Deployment.
3. Create an image-pull Secret in the cluster (or make the GHCR package
   public):

   ```bash
   kubectl -n nginx-app create secret docker-registry ghcr-pull \
     --docker-server=ghcr.io \
     --docker-username=<github-username> \
     --docker-password=<PAT-with-read:packages>
   ```

---

## 4. Trigger the pipeline

```bash
# Edit the page, commit, push:
sed -i 's/running/shipping/' site/index.html
git commit -am "tweak landing copy" && git push
```

In the **Actions** tab you'll see three jobs:

1. **lint** — runs `nginx -t` against the conf inside the same base image
   we ship, and sanity-checks the HTML exists.
2. **build & push image** — Buildx builds the image (with `VERSION` and
   `COMMIT` baked into `/version.json`), tags it `sha-<commit>` and
   `latest`, pushes to GHCR.
3. **deploy** — `kubectl set image` to the new image **by digest**, waits
   for rollout, runs an in-cluster smoke test against `/health`,
   `/version.json`, and `/`.

---

## 5. Verify after deploy

```bash
kubectl -n nginx-app get pods
kubectl -n nginx-app logs -l app.kubernetes.io/name=nginx-app --tail=50
kubectl -n nginx-app rollout history deploy/nginx-app

kubectl -n nginx-app port-forward svc/nginx-app 8080:80
curl localhost:8080/version.json     # see the new commit
```

Roll back if needed:

```bash
kubectl -n nginx-app rollout undo deploy/nginx-app
```

---

## 6. Variations you might want

- **No custom image — content from a ConfigMap.** Use the official
  `nginx:1.27-alpine` image and mount the HTML from a ConfigMap. Faster
  iteration on content, but the rollout won't restart Pods automatically
  when the ConfigMap changes (you'd add a checksum annotation to the Pod
  template, or run `kubectl rollout restart`).
- **TLS at the edge.** Add an Ingress with a cert-manager-issued cert
  instead of a ClusterIP + port-forward.
- **Behind a CDN.** Strip the `/health` location from public access in
  your edge config; keep it open inside the cluster for probes.
- **Multi-arch image.** Set `platforms: linux/amd64,linux/arm64` on the
  `docker/build-push-action` step.
