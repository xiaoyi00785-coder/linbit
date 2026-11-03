# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/asset/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/asset ./asset
WORKDIR /src/asset
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/asset .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/asset /asset
EXPOSE 8082
ENTRYPOINT ["/asset"]
