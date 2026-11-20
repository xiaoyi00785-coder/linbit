# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/web3wallet/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/web3wallet ./web3wallet
WORKDIR /src/web3wallet
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/web3wallet .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/web3wallet /web3wallet
EXPOSE 8097
ENTRYPOINT ["/web3wallet"]
