# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/kyc/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/kyc ./kyc
WORKDIR /src/kyc
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/kyc .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/kyc /kyc
EXPOSE 8087
ENTRYPOINT ["/kyc"]
