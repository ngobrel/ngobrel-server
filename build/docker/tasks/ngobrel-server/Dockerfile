FROM alpine:latest
RUN apk --update add ca-certificates curl
COPY ./build/docker/ngobrel-server /bin/ngobrel-server
COPY ./build/docker/tasks/ngobrel-server/ngobrel-server.sh /bin/ngobrel-server.sh
RUN mkdir /migrate
COPY ./scripts/migrate* /migrate/
RUN chmod +x /bin/ngobrel-server.sh
ENTRYPOINT [ "/bin/ngobrel-server.sh" ]
