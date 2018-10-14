#!/bin/sh

if [ -f /conf/settings.sh ];then
    . /conf/settings.sh
fi

cd /deploy
./deploy.sh

/bin/ngobrel-server