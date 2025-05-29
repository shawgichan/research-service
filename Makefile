exp:
	export $$(grep -v '^#' app.env | xargs)
postgres:
	docker stop db_research || true
	docker rm db_research || true
	docker run --name db_research -e POSTGRES_USER=user -e POSTGRES_PASSWORD=secret -p 5432:5432 -d postgres:17-alpine
createdb:
	@docker exec -it db_research dropdb --if-exists -U ${POSTGRES_USER} research_db || true
	@docker exec -it db_research createdb --username=${POSTGRES_USER} --owner=${POSTGRES_USER} research_db
migrateup1:
	migrate -path internal/db/migration -database "postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:5432/research_db?sslmode=disable" -verbose up 1
migrateup:
	migrate -path internal/db/migration -database "postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:5432/research_db?sslmode=disable" -verbose up
migratedown:
	migrate -path internal/db/migration -database "postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:5432/research_db?sslmode=disable" -verbose down
migratedown1:
	migrate -path internal/db/migration -database "postgresql://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:5432/research_db?sslmode=disable" -verbose down 1
redis:
	docker run --name redis -p 6379:6379 -d redis:7-alpine
run:
	docker compose up --build
test:
	go test -v -cover $$(go list ./... | grep -v /mail)
mock:
	mockgen -package mockdb -destination internal/db/mock/store.go exam-dashboard/internal/db/sqlc Store
.PHONY: createdb migrateup migratedown postgres redis run mock migratedown1 migrateup1 test