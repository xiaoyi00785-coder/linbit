# Build context is the repository root; common is a local module replacement.
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/paper ./paper
WORKDIR /src/paper
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/paper .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/paper /paper
EXPOSE 8094
ENTRYPOINT ["/paper"]
