set -eu

run() {
	mysql -uroot -p"$MYSQL_ROOT_PASSWORD" "$@"
}

for db in $DATABASES; do
	run -e "CREATE DATABASE IF NOT EXISTS \`$db\`; GRANT ALL PRIVILEGES ON \`$db\`.* TO '$APP_USER'@'%';"
	for migration in /migrations/*.sql; do
		run "$db" < "$migration"
	done
	echo "migrated $db"
done
