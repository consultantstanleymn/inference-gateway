# Build stage: cgo is needed for ONNX Runtime bindings.
FROM docker.io/library/golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN scripts/fetch-onnxruntime.sh \
 && CGO_ENABLED=1 go build -trimpath -o /out/gateway ./cmd/gateway

# Run stage: distroless "cc" has glibc and libstdc++, which ONNX Runtime needs. Non-root by default.
FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=build /out/gateway /usr/local/bin/gateway
COPY --from=build /src/third_party/libonnxruntime.so /opt/ort/libonnxruntime.so
COPY models/v1/model.onnx models/v1/meta.json /models/v1/
COPY models/v2/model.onnx models/v2/meta.json /models/v2/
ENV GATEWAY_ORT_LIB=/opt/ort/libonnxruntime.so \
    GATEWAY_MODEL_DIR=/models/v1
EXPOSE 8080 9090
ENTRYPOINT ["/usr/local/bin/gateway"]
