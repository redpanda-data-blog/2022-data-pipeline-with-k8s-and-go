FROM golang as build-stage

RUN go env -w GOPROXY=direct

WORKDIR /app

COPY go.mod /app
COPY go.sum /app
RUN go mod download

COPY main.go /app

RUN cd /app && CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o consumer

FROM alpine
COPY --from=build-stage /app/consumer /
CMD ["/consumer"]