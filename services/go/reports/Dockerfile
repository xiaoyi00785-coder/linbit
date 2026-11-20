# Build context must be the repository root so the shared ../common module is
# available:  docker build -f services/go/reports/Dockerfile .
FROM golang:1.27 AS build
WORKDIR /src
COPY services/go/common ./common
COPY services/go/reports ./reports
WORKDIR /src/reports
RUN go mod download
RUN CGO_ENABLED=0 go build -o /out/reports .

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/reports /reports
EXPOSE 8098
VOLUME ["/data/exports"]
ENTRYPOINT ["/reports"]
