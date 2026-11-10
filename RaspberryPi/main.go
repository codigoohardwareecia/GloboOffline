package main

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	// Driver SQLite 100% Go puro (sem CGO, funciona no XP e no Windows 11)
	_ "modernc.org/sqlite"

	"golang.org/x/image/draw"
)

const (
	mbtilesPath = `/home/principal/mbtiles-server/mbtiles/2019-10-south-america-14.mbtiles`
)

var caminhoMasterMbTiles string

func main() {
	execDir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Erro ao obter diretório atual: %v", err)
	}

	pastaMbTilesMaster := filepath.Join(execDir, "MasterMbTiles")
	if err := os.MkdirAll(pastaMbTilesMaster, 0755); err != nil {
		log.Fatalf("Erro ao criar diretório MasterMbTiles: %v", err)
	}
	caminhoMasterMbTiles = filepath.Join(pastaMbTilesMaster, "master_tactical.mbtiles")

	inicializarBancoMestreUnificado(caminhoMasterMbTiles)

	mux := http.NewServeMux()

	// Servir arquivos estáticos (wwwroot)
	fs := http.FileServer(http.Dir("./wwwroot"))
	mux.Handle("/", fs)

	// Rotas universais (compatíveis com Go 1.10 e Go 1.22+)
	mux.HandleFunc("/data/v3/", handleGetMapTile)
	mux.HandleFunc("/fonts/", handleGetFont)
	mux.HandleFunc("/api/tactical/upload", handleUploadTactical)
	mux.HandleFunc("/api/tactical/tiles/", handleGetTacticalTile)

	handler := enableCORS(mux)

	fmt.Println("Servidor Go rodando em http://localhost:8080 (Compatível com Windows XP e Windows 10/11)")
	if err := http.ListenAndServe(":8080", handler); err != nil {
		log.Fatalf("Erro no servidor HTTP: %v", err)
	}
}

func enableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "*")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Endpoint: GET /data/v3/{z}/{x}/{y}.pbf
func handleGetMapTile(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/data/v3/"), "/")
	if len(parts) < 3 {
		http.NotFound(w, r)
		return
	}

	z, _ := strconv.Atoi(parts[0])
	x, _ := strconv.Atoi(parts[1])
	yStr := strings.TrimSuffix(parts[2], ".pbf")
	y, err := strconv.Atoi(yStr)
	if err != nil {
		http.Error(w, "Y inválido", http.StatusBadRequest)
		return
	}

	invertedY := int(math.Pow(2, float64(z)) - 1 - float64(y))

	db, err := sql.Open("sqlite", mbtilesPath+"?mode=ro")
	if err != nil {
		http.Error(w, "Erro no banco", http.StatusInternalServerError)
		return
	}
	defer db.Close()

	query := "SELECT tile_data FROM tiles WHERE zoom_level = ? AND tile_column = ? AND tile_row = ?"
	var tileBytes []byte
	err = db.QueryRow(query, z, x, invertedY).Scan(&tileBytes)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	finalBytes := tileBytes
	if len(tileBytes) > 2 && tileBytes[0] == 0x1f && tileBytes[1] == 0x8b {
		gzReader, err := gzip.NewReader(bytes.NewReader(tileBytes))
		if err == nil {
			decompressed, err := io.ReadAll(gzReader)
			if err == nil {
				finalBytes = decompressed
			}
			gzReader.Close()
		}
	}

	w.Header().Set("Content-Encoding", "identity")
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.Write(finalBytes)
}

// Endpoint: GET /fonts/{fontstack}/{range}.pbf
func handleGetFont(w http.ResponseWriter, r *http.Request) {
	pathStr := strings.TrimPrefix(r.URL.Path, "/fonts/")
	parts := strings.Split(pathStr, "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}

	fontstack := parts[0]
	rangeVal := strings.TrimSuffix(parts[1], ".pbf")

	execDir, _ := os.Getwd()
	fontPath := filepath.Join(execDir, "fonts", fontstack, fmt.Sprintf("%s.pbf", rangeVal))

	bytesData, err := os.ReadFile(fontPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Encoding", "identity")
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.Write(bytesData)
}

// Endpoint: POST /api/tactical/upload
func handleUploadTactical(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseMultipartForm(10 << 20)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Formato inválido."})
		return
	}

	latStr := r.FormValue("latitude")
	lonStr := r.FormValue("longitude")
	zoomStr := r.FormValue("zoom")
	camada := r.FormValue("camada")

	file, fileHeader, err := r.FormFile("imagem")
	if err != nil || fileHeader.Size == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Nenhuma imagem enviada."})
		return
	}
	defer file.Close()

	latitude, errLat := strconv.ParseFloat(latStr, 64)
	longitude, errLon := strconv.ParseFloat(lonStr, 64)
	rawZoom, errZoom := strconv.ParseFloat(zoomStr, 64)

	if errLat != nil || errLon != nil || errZoom != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Coordenadas ou Zoom inválidos."})
		return
	}

	targetZoom := int(math.Round(rawZoom)) + 1
	if targetZoom < 0 {
		targetZoom = 0
	}
	if targetZoom > 30 {
		targetZoom = 30
	}

	execDir, _ := os.Getwd()
	pastaUploads := filepath.Join(execDir, "UploadsTactical")
	_ = os.MkdirAll(pastaUploads, 0755)

	nomeArquivoUnico := fmt.Sprintf("%s_%s", time.Now().Format("20060102_150405"), fileHeader.Filename)
	caminhoImagem := filepath.Join(pastaUploads, nomeArquivoUnico)

	dstFile, err := os.Create(caminhoImagem)
	if err != nil {
		http.Error(w, "Erro ao salvar arquivo", http.StatusInternalServerError)
		return
	}
	io.Copy(dstFile, file)
	dstFile.Close()

	err = adicionarTileAoBancoMestreAcumulativo(caminhoImagem, caminhoMasterMbTiles, latitude, longitude, targetZoom)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"message":     "Camada tática acumulada com sucesso!",
		"coordinates": map[string]float64{"latitude": latitude, "longitude": longitude},
		"zoom":        targetZoom,
		"layer":       camada,
	})
}

// Endpoint: GET /api/tactical/tiles/{z}/{x}/{y}
func handleGetTacticalTile(w http.ResponseWriter, r *http.Request) {
	if _, err := os.Stat(caminhoMasterMbTiles); os.IsNotExist(err) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/tactical/tiles/"), "/")
	if len(parts) < 3 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	z, _ := strconv.Atoi(parts[0])
	x, _ := strconv.Atoi(parts[1])
	yStr := strings.TrimSuffix(parts[2], ".png")
	y, _ := strconv.Atoi(yStr)

	invertedY := int(math.Pow(2, float64(z)) - 1 - float64(y))

	db, err := sql.Open("sqlite", caminhoMasterMbTiles+"?mode=ro")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer db.Close()

	query := "SELECT tile_data FROM tiles WHERE zoom_level = ? AND tile_column = ? AND tile_row = ?"
	var tileBytes []byte
	err = db.QueryRow(query, z, x, invertedY).Scan(&tileBytes)
	if err != nil {
		_ = db.QueryRow(query, z, x, y).Scan(&tileBytes)
	}

	if len(tileBytes) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(tileBytes)
}

func inicializarBancoMestreUnificado(caminhoDb string) {
	if _, err := os.Stat(caminhoDb); os.IsNotExist(err) {
		db, err := sql.Open("sqlite", caminhoDb)
		if err != nil {
			log.Fatalf("Erro ao criar banco mestre: %v", err)
		}
		defer db.Close()

		sqlStmt := `
			CREATE TABLE metadata (name TEXT, value TEXT);
			CREATE TABLE tiles (zoom_level INTEGER, tile_column INTEGER, tile_row INTEGER, tile_data BLOB);
			CREATE UNIQUE INDEX tile_index on tiles (zoom_level, tile_column, tile_row);
			
			INSERT INTO metadata (name, value) VALUES ('name', 'OSIRIS Master Tactical Overlay Cumulative');
			INSERT INTO metadata (name, value) VALUES ('format', 'png');
			INSERT INTO metadata (name, value) VALUES ('bounds', '-180,-85.0511,180,85.0511');
			INSERT INTO metadata (name, value) VALUES ('center', '0,0,0');
			INSERT INTO metadata (name, value) VALUES ('minzoom', '0');
			INSERT INTO metadata (name, value) VALUES ('maxzoom', '30');
			INSERT INTO metadata (name, value) VALUES ('type', 'overlay');
		`
		_, err = db.Exec(sqlStmt)
		if err != nil {
			log.Fatalf("Erro ao inicializar schema: %v", err)
		}
	}
}

func adicionarTileAoBancoMestreAcumulativo(caminhoImagem, caminhoDb string, lat, lon float64, zoom int) error {
	db, err := sql.Open("sqlite", caminhoDb)
	if err != nil {
		return err
	}
	defer db.Close()

	tileX := longitudeToTileX(lon, zoom)
	tileY := latitudeToTileY(lat, zoom)

	imgFile, err := os.Open(caminhoImagem)
	if err != nil {
		return err
	}
	defer imgFile.Close()

	imgOriginal, _, err := image.Decode(imgFile)
	if err != nil {
		return err
	}

	dstBounds := image.Rect(0, 0, 256, 256)
	tileBitmap := image.NewRGBA(dstBounds)
	draw.BiLinear.Scale(tileBitmap, dstBounds, imgOriginal, imgOriginal.Bounds(), draw.Over, nil)

	var buf bytes.Buffer
	if err := png.Encode(&buf, tileBitmap); err != nil {
		return err
	}
	tileBytes := buf.Bytes()

	maxTiles := int(math.Pow(2, float64(zoom)))
	tileRowTms := (maxTiles - 1) - int(tileY)

	insertTileSql := "INSERT OR REPLACE INTO tiles (zoom_level, tile_column, tile_row, tile_data) VALUES (?, ?, ?, ?);"
	_, err = db.Exec(insertTileSql, zoom, int(tileX), tileRowTms, tileBytes)
	return err
}

func longitudeToTileX(lon float64, zoom int) int64 {
	return int64((lon + 180.0) / 360.0 * math.Pow(2, float64(zoom)))
}

func latitudeToTileY(lat float64, zoom int) int64 {
	latRad := lat * math.Pi / 180.0
	return int64((1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * math.Pow(2, float64(zoom)))
}
