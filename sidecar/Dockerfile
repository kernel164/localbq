FROM ubuntu:22.04 AS builder

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=UTC
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential ca-certificates curl git python3 tzdata unzip \
    && rm -rf /var/lib/apt/lists/*

ARG TARGETARCH
RUN ARCH=$(case "${TARGETARCH}" in arm64) echo "arm64" ;; *) echo "amd64" ;; esac) && \
    curl -fsSL "https://github.com/bazelbuild/bazelisk/releases/latest/download/bazelisk-linux-${ARCH}" \
    -o /usr/local/bin/bazel && chmod +x /usr/local/bin/bazel

ARG GOOGLESQL_VERSION=2026.01.1
RUN git clone --depth 1 --branch ${GOOGLESQL_VERSION} \
    https://github.com/google/googlesql.git /googlesql

WORKDIR /googlesql

# Place sidecar INSIDE the googlesql tree (visibility: //googlesql/...)
COPY sidecar/googlesql_server.cc /googlesql/googlesql/sidecar/googlesql_server.cc
COPY sidecar/BUILD.bazel /googlesql/googlesql/sidecar/BUILD.bazel

# Build (target path is //googlesql/sidecar since it's inside the tree)
# --spawn_strategy=local disables Bazel sandboxing to avoid rules_foreign_cc
# failures on ARM64 Docker where the sandbox breaks C compiler detection.
RUN bazel build -c opt //googlesql/sidecar:googlesql_server \
    --action_env=CC=gcc --action_env=CXX=g++ \
    --spawn_strategy=local

RUN mkdir -p /output && \
    cp bazel-bin/googlesql/sidecar/googlesql_server /output/googlesql_server

FROM ubuntu:22.04 AS runtime
ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=UTC
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata && rm -rf /var/lib/apt/lists/*
COPY --from=builder /output/googlesql_server /usr/local/bin/googlesql_server
EXPOSE 50051
ENTRYPOINT ["googlesql_server"]
CMD ["--port=50051"]
