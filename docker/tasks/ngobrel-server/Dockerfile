FROM alpine:latest
RUN apk --update add ca-certificates
COPY ./docker/ngobrel-server /bin/ngobrel-server
COPY ./docker/tasks/ngobrel-server/ngobrel-server.sh /bin/ngobrel-server.sh
COPY ./deploy /deploy
RUN chmod +x /bin/ngobrel-server.sh
ENTRYPOINT [ "/bin/ngobrel-server.sh" ]
