FROM golang:1.21-alpine

WORKDIR /app

COPY go.mod ./
RUN go mod download all

COPY main.go ./
RUN go mod tidy
RUN go build -o bot .

CMD ["./bot"]
