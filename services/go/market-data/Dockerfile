# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/market-data/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/market-data ./market-data
WORKDIR /src/market-data
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/market-data .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/market-data /market-data
EXPOSE 8085
ENTRYPOINT ["/market-data"]
