#FROM golang:1.25.12-alpine AS build
#
#ARG TARGETARCH
#ARG GRPC_HEALTH_PROBE_VERSION=v0.3.2
#
#WORKDIR /go/src/golog
#COPY . .
#RUN CGO_ENABLED=0 go build -o /go/bin/golog ./cmd/golog
#
#RUN wget -qO /go/bin/grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-${TARGETARCH} && \
#    chmod +x /go/bin/grpc_health_probe
#
#FROM scratch
#COPY --from=build /go/bin/golog /bin/golog
#COPY --from=build /go/bin/grpc_health_probe /bin/grpc_health_probe
#ENTRYPOINT ["/bin/golog"]
FROM golang:1.25.12-alpine AS build

ARG TARGETARCH
ARG GRPC_HEALTH_PROBE_VERSION=v0.3.2

WORKDIR /go/src/golog
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH}" \
    go build -o /go/bin/golog ./cmd/golog

RUN wget -qO /go/bin/grpc_health_probe \
    "https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-${TARGETARCH}" \
    && chmod +x /go/bin/grpc_health_probe

FROM scratch

COPY --from=build /go/bin/golog /bin/golog
COPY --from=build /go/bin/grpc_health_probe /bin/grpc_health_probe

ENTRYPOINT ["/bin/golog"]