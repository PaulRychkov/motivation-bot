package main

import (
	"context"
	"os"
)

type App struct {
	ctx context.Context
}

func NewApp() *App {
	return &App{}
}

func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) GetBackendURL() string {
	if url := os.Getenv("CHAT_API_URL"); url != "" {
		return url
	}
	return "http://localhost:18084"
}

func (a *App) GetToken() string {
	return os.Getenv("CHAT_TOKEN")
}
