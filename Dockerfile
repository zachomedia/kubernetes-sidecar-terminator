# Build with the golang image
FROM golang:1.12-alpine AS build

ENV GO111MODULE on

# Add git
RUN apk add git

# Set workdir
WORKDIR /go/src/github.com/zachomedia/kubernetes-sidecar-terminator

# Add dependencies
COPY go.mod .
COPY go.sum .
RUN go mod download

# Build
COPY . .
RUN CGO_ENABLED=0 go install .

# Generate final image
FROM scratch
COPY --from=build /go/bin/kubernetes-sidecar-terminator /kubernetes-sidecar-terminator
ENTRYPOINT [ "/kubernetes-sidecar-terminator" ]
