# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/nft/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/nft ./nft
WORKDIR /src/nft
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/nft .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/nft /nft
EXPOSE 8095
ENTRYPOINT ["/nft"]
