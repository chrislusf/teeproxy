FROM ubuntu:15.10

RUN apt-get -y update && \
    apt-get -y install gccgo && \
    apt-get -y autoremove

WORKDIR /usr/local/src
COPY . /usr/local/src
RUN go build teeproxy.go

ENTRYPOINT ["/usr/local/src/teeproxy"]



