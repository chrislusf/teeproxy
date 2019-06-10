FROM golang:latest as build-env

WORKDIR /usr/local/src/
COPY teeproxy.go /usr/local/src/
RUN  CGO_ENABLED=0 go build teeproxy.go

FROM gcr.io/distroless/static
COPY --from=build-env /usr/local/src/teeproxy .
CMD ["/teeproxy"]
