package repository

type Config struct {
	PostgresURL   string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
}
