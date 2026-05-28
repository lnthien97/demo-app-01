# syntax=docker/dockerfile:1.7

# ---- Build stage ------------------------------------------------------------
FROM golang:1.22-alpine AS build
WORKDIR /src

# Cache deps first.
COPY app/go.mod ./app/
RUN cd app && go mod download

COPY app/ ./app/

# Build a fully static binary so we can run on distroless/static.
ARG VERSION=dev
ARG COMMIT=unknown
RUN cd app && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
      -o /out/demo-app .

# ---- Runtime stage ----------------------------------------------------------
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=build /out/demo-app /demo-app

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/demo-app"]
