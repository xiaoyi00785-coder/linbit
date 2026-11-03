# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/marketmaker/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/marketmaker ./marketmaker
WORKDIR /src/marketmaker
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/marketmaker .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/marketmaker /marketmaker
EXPOSE 8093
ENTRYPOINT ["/marketmaker"]
