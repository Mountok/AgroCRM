FROM golang:1.24-alpine AS build
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN go build -o /server ./cmd/server

FROM alpine:3.20
WORKDIR /app
COPY --from=build /server /server
EXPOSE 8080
CMD ["/server"]
