# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/gateway/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/gateway ./gateway
WORKDIR /src/gateway
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/gateway .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/gateway /gateway
EXPOSE 8080
ENTRYPOINT ["/gateway"]
