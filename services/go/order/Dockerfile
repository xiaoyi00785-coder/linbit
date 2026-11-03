# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/order/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/order ./order
WORKDIR /src/order
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/order .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/order /order
EXPOSE 8084
ENTRYPOINT ["/order"]
