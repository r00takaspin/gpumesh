# Stage 1: build
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/gpumesh ./cmd/coordinator

# Stage 2: minimal runtime
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /bin/gpumesh /bin/gpumesh
EXPOSE 8080
ENTRYPOINT ["/bin/gpumesh"]
