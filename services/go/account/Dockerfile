# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/account/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/account ./account
WORKDIR /src/account
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/account .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/account /account
EXPOSE 8081
ENTRYPOINT ["/account"]
