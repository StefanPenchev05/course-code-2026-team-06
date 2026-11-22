FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/ ./cmd/api ./cmd/worker ./cmd/semester-maintenance

# Optional local/CI toolchain. It never becomes part of the runtime image.
FROM build AS test
RUN apk add --no-cache bash gcc musl-dev python3 postgresql-client docker-cli
RUN go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.3
COPY tests ./tests
COPY migrations ./migrations
COPY ops ./ops
# Windows checkouts may use CRLF. Normalize only the copied test sources.
RUN find cmd internal tests migrations ops -type f -exec sed -i 's/\r$//' {} +
CMD ["bash", "tests/integration/test_docker.sh"]

FROM scratch AS runtime
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/api /tutti-api
COPY --from=build /out/worker /tutti-worker
COPY --from=build /out/semester-maintenance /semester-maintenance
USER 65532:65532
CMD ["/tutti-api"]
