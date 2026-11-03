# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/strategy/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/strategy ./strategy
WORKDIR /src/strategy
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/strategy .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/strategy /strategy
EXPOSE 8090
ENTRYPOINT ["/strategy"]
