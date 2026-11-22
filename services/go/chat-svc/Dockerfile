# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/chat-svc/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/chat-svc ./chat-svc
WORKDIR /src/chat-svc
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/chat-svc .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/chat-svc /chat-svc
EXPOSE 8095
ENTRYPOINT ["/chat-svc"]
