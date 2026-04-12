FROM ubuntu:22.04 AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential ca-certificates curl git python3 tzdata unzip \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL https://github.com/bazelbuild/bazelisk/releases/latest/download/bazelisk-linux-amd64 \
    -o /usr/local/bin/bazel && chmod +x /usr/local/bin/bazel

ARG GOOGLESQL_VERSION=2026.01.1
RUN git clone --depth 1 --branch ${GOOGLESQL_VERSION} \
    https://github.com/google/googlesql.git /googlesql

WORKDIR /googlesql

# Place sidecar INSIDE the googlesql tree (visibility: //googlesql/...)
COPY sidecar/googlesql_server.cc /googlesql/googlesql/sidecar/googlesql_server.cc
COPY sidecar/BUILD.bazel /googlesql/googlesql/sidecar/BUILD.bazel

# Build (target path is //googlesql/sidecar since it's inside the tree)
RUN bazel build -c opt //googlesql/sidecar:googlesql_server \
    --action_env=CC=gcc --action_env=CXX=g++

RUN mkdir -p /output && \
    cp bazel-bin/googlesql/sidecar/googlesql_server /output/googlesql_server

FROM ubuntu:22.04 AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates tzdata && rm -rf /var/lib/apt/lists/*
COPY --from=builder /output/googlesql_server /usr/local/bin/googlesql_server
EXPOSE 50051
ENTRYPOINT ["googlesql_server"]
CMD ["--port=50051"]
