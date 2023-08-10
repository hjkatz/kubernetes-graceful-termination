FROM alpine:3.18

RUN adduser -D graceful-terminator

ADD ./bin/linux/amd64/graceful-terminator /bin/graceful-terminator

USER graceful-terminator

ENTRYPOINT ["/bin/graceful-terminator"]
