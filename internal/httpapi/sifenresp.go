package httpapi

import "github.com/DoomsDV/firmador-e/internal/sifen"

// Aliases locales para no tocar todo el package de una vez.
type sifenResult = sifen.Result

func parseSifenResult(body []byte) sifenResult { return sifen.ParseResult(body) }
func estadoDE(codRes string) string             { return sifen.EstadoDE(codRes) }
func estadoEvento(codRes string) string         { return sifen.EstadoEvento(codRes) }
