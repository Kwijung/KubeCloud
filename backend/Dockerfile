FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/kloudia-api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/kloudia-api /kloudia-api
EXPOSE 8080
ENTRYPOINT ["/kloudia-api"]
