DB_NAME=${DB_NAME:=ngobrel}
DB_USER=${DB_USER:=dbuser}
DB_PASS=${DB_PASS:=dbpass}
DB_HOST=${DB_HOST:=localhost}
DB_PORT=${DB_PORT:=5432}
DB_URL=postgres://$DB_USER:$DB_PASS@$DB_HOST:$DB_PORT/$DB_NAME?sslmode=disable
export DB_URL