FROM golang:1.23.5 AS build

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/pasty

# runtime container
FROM alpine:latest
WORKDIR /app

COPY --from=build /app/pasty /app/pasty
COPY templates/ templates/
RUN mkdir uploads
EXPOSE 8090
CMD ["/app/pasty"]
