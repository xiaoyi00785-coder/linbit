# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/wallet/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/wallet ./wallet
WORKDIR /src/wallet
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/wallet .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/wallet /wallet
EXPOSE 8086
ENTRYPOINT ["/wallet"]
