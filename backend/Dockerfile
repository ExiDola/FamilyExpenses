FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/family-expenses ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /out/family-expenses /app/family-expenses
COPY migrations /app/migrations
ENV TZ=Europe/Kyiv
EXPOSE 8080
ENTRYPOINT ["/app/family-expenses"]
