FROM golang:1-alpine as builder
COPY . /src
WORKDIR /src
RUN go mod tidy
RUN go mod download
RUN go build -o /bin/seaweedfs-volume-plugin

FROM alpine:3
RUN apk add --no-cache socat fuse
RUN mkdir -p /run/docker/plugins /mnt/state /mnt/volumes
COPY --from=chrislusf/seaweedfs:2.83 /usr/bin/weed /usr/bin/
COPY --from=builder /bin/seaweedfs-volume-plugin /bin/seaweedfs-volume-plugin
CMD ["/bin/seaweedfs-volume-plugin"]

