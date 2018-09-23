#!/bin/sh

NOW=`pwd`
DIR=`dirname $0`
cd $DIR
if ! [ -f migrate.linux-amd64 ];then
    curl -o - -O -J -L https://github.com/golang-migrate/migrate/releases/download/v3.4.0/migrate.linux-amd64.tar.gz | tar xzf -
fi
. ./settings.sh
./migrate.linux-amd64 -path=./migrate -database $DB_URL up $DB_MIGRATE
cd $NOW