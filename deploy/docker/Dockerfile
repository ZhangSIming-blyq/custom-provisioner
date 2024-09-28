FROM golang:1.23 as builder
WORKDIR /workspace
COPY . .
WORKDIR /workspace/cmd/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o custom-provisioner .

FROM alpine:3.14
COPY --from=builder /workspace/cmd/custom-provisioner /custom-provisioner
ENTRYPOINT ["/custom-provisioner"]