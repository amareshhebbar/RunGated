ARG GO_VERSION=1.24
ARG NODE_VERSION=22

FROM node:${NODE_VERSION}-alpine AS console
WORKDIR /src/console
RUN corepack enable
COPY console/package.json console/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY console ./
RUN pnpm build

FROM golang:${GO_VERSION}-bookworm AS build
ARG ONNX=false
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN rm -rf internal/consoleui/assets
COPY --from=console /src/console/out internal/consoleui/assets
ARG EDITION=free
RUN TAGS=""; [ "$EDITION" = "enterprise" ] && TAGS="enterprise"; \
    if [ "$ONNX" = "true" ]; then \
      CGO_ENABLED=1 go build -trimpath -tags "onnx${TAGS:+,$TAGS}" -ldflags "-s -w -X main.version=${VERSION}" -o /out/rungated ./cmd/rungated; \
    else \
      CGO_ENABLED=0 go build -trimpath ${TAGS:+-tags $TAGS} -ldflags "-s -w -X main.version=${VERSION}" -o /out/rungated ./cmd/rungated; \
    fi
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/rungated /rungated
COPY --from=build --chown=65532:65532 /out/data /data
COPY spec/ats/examples /spec/ats/examples
ENV RUNGATED_DATA_DIR=/data
VOLUME ["/data"]
WORKDIR /
EXPOSE 8080 8081
ENTRYPOINT ["/rungated"]
CMD ["serve"]

FROM debian:bookworm-slim AS runtime-onnx
ARG ONNXRUNTIME_VERSION=1.30.0
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl \
    && curl -fsSL "https://github.com/microsoft/onnxruntime/releases/download/v${ONNXRUNTIME_VERSION}/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}.tgz" \
       | tar -xz -C /opt \
    && ln -s /opt/onnxruntime-linux-x64-${ONNXRUNTIME_VERSION}/lib/libonnxruntime.so /usr/local/lib/libonnxruntime.so \
    && ldconfig \
    && apt-get purge -y curl && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*
RUN useradd --system --uid 65532 --no-create-home rungated
COPY --from=build /out/rungated /rungated
COPY --from=build --chown=65532:65532 /out/data /data
COPY spec/ats/examples /spec/ats/examples
ENV ORT_LIB=/usr/local/lib/libonnxruntime.so
ENV RUNGATED_DATA_DIR=/data
VOLUME ["/data"]
USER 65532
EXPOSE 8080 8081
ENTRYPOINT ["/rungated"]
CMD ["serve"]