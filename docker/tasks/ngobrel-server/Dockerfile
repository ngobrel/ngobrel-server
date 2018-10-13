FROM alpine:latest
COPY ./docker/ngobrel-server /bin/ngobrel-server
COPY ./docker/tasks/ngobrel-server/ngobrel-server.sh /bin/ngobrel-server.sh
RUN chmod +x /bin/ngobrel-server.sh
ENTRYPOINT [ "/bin/ngobrel-server.sh" ]
