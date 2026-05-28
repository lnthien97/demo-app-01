# syntax=docker/dockerfile:1.7
#
# nginx-app — a static site served by nginx, packaged for Kubernetes.
#
# Uses nginxinc/nginx-unprivileged so the container listens on :8080 and runs
# as a non-root user out of the box (works with runAsNonRoot: true).

FROM nginxinc/nginx-unprivileged:1.27-alpine

ARG VERSION=dev
ARG COMMIT=unknown

# Drop the default config, ship ours.
USER root
RUN rm -f /etc/nginx/nginx.conf /etc/nginx/conf.d/default.conf
COPY nginx.conf /etc/nginx/nginx.conf

# Static content.
COPY site/index.html         /usr/share/nginx/html/index.html
COPY site/version.json.tmpl  /usr/share/nginx/html/version.json

# Bake build info into version.json.
RUN BUILT="$(date -u +%Y-%m-%dT%H:%M:%SZ)" && \
    sed -i \
      -e "s|__VERSION__|${VERSION}|g" \
      -e "s|__COMMIT__|${COMMIT}|g" \
      -e "s|__BUILT__|${BUILT}|g" \
      /usr/share/nginx/html/version.json && \
    chown -R nginx:nginx /usr/share/nginx/html && \
    nginx -t -c /etc/nginx/nginx.conf

USER nginx
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health || exit 1
