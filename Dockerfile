FROM golang:latest as build-env

WORKDIR /usr/local/src/
COPY teeproxy.go /usr/local/src/
RUN  go build teeproxy.go

FROM gcr.io/distroless/base
COPY --from=build-env /usr/local/src/teeproxy .
CMD ["/teeproxy"]
