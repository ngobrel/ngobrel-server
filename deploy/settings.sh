DB_NAME=${DB_NAME:=ngobrel}
DB_USER=${DB_USER:=dbuser}
DB_PASS=${DB_PASS:=dbpass}
DB_HOST=${DB_HOST:=localhost}
DB_PORT=${DB_PORT:=5432}
DB_URL=postgres://$DB_USER:$DB_PASS@$DB_HOST:$DB_PORT/$DB_NAME?sslmode=disable

REDIS_HOST=${REDIS_HOST:=localhost}
REDIS_PORT=${REDIS_PORT:=6379}
REDIS_URL="$REDIS_HOST:$REDIS_PORT"

SMS_ACCOUNT=${SMS_ACCOUNT:=twilio-account-id}
SMS_TOKEN=${SMS_TOKEN:=twilio-token}

export DB_URL
export REDIS_URL
export SMS_ACCOUNT
export SMS_TOKEN