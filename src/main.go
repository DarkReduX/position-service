package main

import (
	"os"
	"position-service/src/internal/repository"
	"position-service/src/internal/service"
)

const (
	envPostgresURI = "POSTGRES_URI"
)

func main() {
	postgres := repository.NewPostgres(os.Getenv(envPostgresURI))
	service.StartService(postgres)
}
