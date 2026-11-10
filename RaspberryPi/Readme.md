## Mapa - Raspberry Pi - Passo a passo para criar um Globo com mapas Offline
### Passo 1: Downloads necessários

Primeiramente vamos fazer o download dos arquivos que precisamos:

1. Download do arquivo MBTile completo
```
// Link de download com mapas dos continentes
https://archive.org/download/osm-vector-mbtiles/regions/
	
// America do Sul (nivel 14)
https://archive.org/download/osm-vector-mbtiles/regions/2019-09-north-america-14.mbtiles
	
// Planeta (Nivel 14
https://archive.org/download/osm-vector-mbtiles/2020-10-planet-14.mbtiles
	
```
2. Download das Fontes
```
// Link de download raiz
https://github.com/openmaptiles/fonts
	
// Link de Download da versão 2.0;
https://github.com/openmaptiles/fonts/releases/download/v2.0/v2.0.zip
```

3. Download dos arquivos Javascript e CSS do MapLibre: acesse o link a seguir, o arquivo será aberto no navegador, clique com botão direito e clique em "Save As..." selecione o diretório onde salvou os mapas e mantenha o mesmo nome do arquivo, faça isso para o CSS e para o Javascritpt
```
// CSS
https://unpkg.com/maplibre-gl@5.8.0/dist/maplibre-gl.css

// Javascript
https://unpkg.com/maplibre-gl@5.8.0/dist/maplibre-gl.js
```
### PASSO 2: Copiar os arquivos para o Raspberry PI

1. Copie o arquivo mbtile, a pasta fonts, a pasta wwwroot, o arquivo go.mod, go.sum e main.go para um pendrive

2. Montar o pendrive aponando ele para uma pasta
```bash
// Conecte o pendrive e liste s unidades no Putty ou Client SSH
sudo lsblk

// Crie uma pasta que será utilizada para acessar os dados do pendrive
sudo mkdir -p /mnt/pendrive

// Conclui a montagem do pendrive apontando ele para a pasta criada
sudo mount /dev/sda1 /mnt/pendrive	
```
3. Crie uma pasta chamada "mbtiles-server" em /home/principal
```bash
sudo mkdir -p /home/principal/mbtiles-server
```
4. Copiar os arquivos do pendrive para o diretório do usuário
```bash
cp -r /mnt/pendrive/* /home/principal/mbtiles-server/
```


### PASSO 3: Instalando o Go e primeiro Hello World

1. Acesse o raspberry Pi via SSH
2. Instalar o GoLang
```bash
sudo apt install golang-go build-essential gcc -y
```
1. Crie uma pasta e acesse
```bash
mkdir mbtileserver 
cd mbtileserver
```
3. Inicialize a pasta
```bash
go mod init meu-projeto
```
4. Criar um arquivo main.go
```
nano main.go
```

5. Código de exemplo
```Go
package main

import "fmt"

func main() {
    fmt.Println("Olá! Projeto Go rodando no Raspberry Pi!")
}
```

6. Executar o arquivo do projeto
```
go run main.go
```
### PASSO 4: Código

1. Abra o arquivo main.go com o nano
```
nano main.go
```
2. Cole o código Go do nosso projeto
```Go
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
	mbtilesPath = `/home/principal/mbtile-server/mbtiles/2019-10-south-america-14.mbtiles`
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

```
3. Cria uma pasta chamada wwwroot
4. Crie um arquivo index.html  dentro desta pasta
```
nano index.html
```
4. Cole o seguinte código
```Html
<!DOCTYPE html>
<html lang="pt-br">
<head>
  <meta charset="UTF-8">
  <title>Tactical Core - Modo Globo 3D</title>
  
  <!-- MapLibre GL CSS Local -->
  <link href="./maplibre-gl.css" rel="stylesheet" />

  <style>
    body { 
      margin: 0; 
      padding: 0; 
      background-color: #0b0f19; 
      font-family: 'Courier New', Courier, monospace; 
      overflow: hidden; 
    }

    #map { 
      width: 100vw;                     
      height: 100vh;                     
      position: absolute; 
      top: 0; 
      left: 0;               
    }
    
    .hud {
      position: absolute; 
      top: 20px; 
      left: 20px;
      background: rgba(10, 10, 18, 0.95); 
      border: 1px solid #334;
      color: #778899; 
      padding: 15px; 
      font-size: 11px; 
      z-index: 10;
      text-transform: uppercase; 
      border-radius: 4px; 
      pointer-events: none;
      line-height: 1.6; 
      box-shadow: 0 0 15px rgba(0, 136, 255, 0.1);
    }
    .highlight { color: #dfa822; }
    .status-live { color: #55ff55; text-shadow: 0 0 5px rgba(85,255,85,0.5); }

    /* Menu de Contexto Tático */
    .context-menu {
      display: none;
      position: absolute;
      background: rgba(11, 15, 25, 0.98);
      border: 1px solid #00ffff;
      box-shadow: 0 0 15px rgba(0, 255, 255, 0.2);
      border-radius: 4px;
      padding: 5px 0;
      z-index: 1000;
      min-width: 230px;
      font-family: 'Courier New', monospace;
      text-transform: uppercase;
      font-size: 11px;
    }

    .context-menu-item {
      padding: 8px 15px;
      color: #cbd5e1;
      cursor: pointer;
      white-space: nowrap;
    }

    .context-menu-item:hover {
      background-color: rgba(0, 255, 255, 0.15);
      color: #00ffff;
    }

    /* Estrutura do Marcador Permanente */
    .tactical-marker-container {
      display: flex;
      align-items: center;
      gap: 6px;
      cursor: pointer;
    }

    .tactical-dot {
      width: 12px;
      height: 12px;
      background-color: #ffaa00;
      border: 2px solid #fff;
      border-radius: 50%;
      box-shadow: 0 0 10px #ffaa00;
      flex-shrink: 0;
    }

    .click-dot {
      width: 12px;
      height: 12px;
      background-color: #00ffff;
      border: 2px solid #fff;
      border-radius: 50%;
      box-shadow: 0 0 10px #00ffff;
      flex-shrink: 0;
    }

    .tactical-label-box, .click-label-box {
      background-color: rgba(11, 15, 25, 0.95);
      color: #ffffff;
      padding: 4px 8px;
      border: 1px solid #ffaa00;
      font-family: 'Courier New', monospace;
      font-size: 10px;
      border-radius: 3px;
      box-shadow: 0 0 8px rgba(0,0,0,0.6);
      white-space: nowrap;
    }
    .click-label-box { border-color: #00ffff; }

    /* Estilos do Modal de Upload Tático */
    #upload-modal {
      display: none;
      position: fixed;
      top: 0;
      left: 0;
      width: 100vw;
      height: 100vh;
      background: rgba(11, 15, 25, 0.85);
      backdrop-filter: blur(2px);
      z-index: 3000;
      justify-content: center;
      align-items: center;
    }

    .modal-content {
      background: rgba(11, 15, 25, 0.98);
      border: 1px solid #00ffff;
      box-shadow: 0 0 25px rgba(0, 255, 255, 0.25);
      border-radius: 4px;
      padding: 20px;
      width: 380px;
      font-family: 'Courier New', monospace;
      text-transform: uppercase;
      font-size: 11px;
      color: #cbd5e1;
    }

    .modal-content h3 {
      margin-top: 0;
      color: #00ffff;
      font-size: 13px;
      border-bottom: 1px solid #1e293b;
      padding-bottom: 8px;
    }

    .modal-info {
      background: #070a12;
      border: 1px solid #1e293b;
      padding: 10px;
      border-radius: 3px;
      line-height: 1.5;
      margin-top: 10px;
    }

    #file-input {
      width: 100%;
      background: #070a12;
      border: 1px solid #334;
      color: #fff;
      padding: 6px;
      font-family: 'Courier New', monospace;
      font-size: 10px;
      border-radius: 3px;
      box-sizing: border-box;
    }

    .tactical-btn {
      flex: 1;
      padding: 8px;
      font-weight: bold;
      cursor: pointer;
      font-size: 10px;
      font-family: 'Courier New', monospace;
      border-radius: 3px;
      text-transform: uppercase;
    }

    .tactical-btn.confirm {
      background: #00ffff;
      border: none;
      color: #0b0f19;
    }
    .tactical-btn.confirm:hover { background: #38bdf8; }

    .tactical-btn.cancel {
      background: transparent;
      border: 1px solid #ff0055;
      color: #ff0055;
    }
    .tactical-btn.cancel:hover { background: rgba(255, 0, 85, 0.1); }
  </style>

  <!-- MapLibre GL JS Local -->
  <script src="./maplibre-gl.js"></script>
</head>
<body>

  <div class="hud">
    SISTEMA: <span class="highlight">TACTICAL CORE [GLOBE]</span><br>
    STATUS: <span class="status-live">ONLINE (BANCO MESTRE ESTÁVEL)</span><br>
    REGIÃO: SP<br>
    <span style="color: #00ffff;" id="hud-status">[CLIQUE DIREITO NO MAPA]</span><br><br>
    <div style="pointer-events: auto;">
      <input type="text" id="search-input" placeholder="PESQUISAR RUA..." style="background: #0b0f19; border: 1px solid #00ffff; color: #fff; padding: 4px; font-family: 'Courier New'; font-size: 10px; width: 150px; text-transform: uppercase;">
      <button id="search-btn" style="background: #00ffff; border: none; color: #0b0f19; padding: 4px 8px; font-weight: bold; cursor: pointer; font-size: 10px;">IR</button>
    </div>
  </div>

  <div id="map"></div>

  <!-- Menu de Contexto Customizado -->
  <div id="context-menu" class="context-menu">
    <div class="context-menu-item" id="menu-marcador">📍 Fixar Ponto Tático</div>
    <div class="context-menu-item" id="menu-rota-origem">🎯 Definir Rota (Origem)</div>
    <div class="context-menu-item" id="menu-rota-destino" style="display:none; color: #ff0055;">🏁 Definir Rota (Destino)</div>
    <div class="context-menu-item" id="menu-info">📊 Inspecionar Via (JSON)</div>
    <div class="context-menu-item" id="menu-upload" style="color: #00ffaa;">📷 Upload de Imagem no Alvo</div>
  </div>

  <!-- Modal de Upload Tático -->
  <div id="upload-modal">
    <div class="modal-content">
      <h3>[ UPLOAD DE REGISTRO TÁTICO ]</h3>
      <div class="modal-info">
        <div><strong>LATITUDE:</strong> <span id="modal-lat" class="highlight">---</span></div>
        <div><strong>LONGITUDE:</strong> <span id="modal-lng" class="highlight">---</span></div>
        <div><strong>CAMADA ATIVA:</strong> <span id="modal-layer" style="color: #00ffff;">---</span></div>
      </div>
      
      <div style="margin: 15px 0;">
        <label style="display: block; margin-bottom: 5px; color: #94a3b8;">SELECIONAR ARQUIVO DE IMAGEM:</label>
        <input type="file" id="file-input" accept="image/*">
      </div>

      <div style="display: flex; gap: 10px; margin-top: 20px;">
        <button id="upload-submit" class="tactical-btn confirm">ENVIAR ARQUIVO</button>
        <button id="upload-close" class="tactical-btn cancel">CANCELAR</button>
      </div>
    </div>
  </div>

  <script>
    const baseUrl = window.location.origin;

    const map = new maplibregl.Map({
      container: 'map',
      style: {
        version: 8,
        glyphs: baseUrl + '/fonts/{fontstack}/{range}.pbf',
        sources: {
          'osm-vector': {
            type: 'vector',
            tiles: [baseUrl + '/data/v3/{z}/{x}/{y}.pbf'],
            minzoom: 0,
            maxzoom: 14 
          },
          'tactical-master-source': {
            type: 'raster',
            tiles: [baseUrl + '/api/tactical/tiles/{z}/{x}/{y}'],
            tileSize: 256,
            minzoom: 0,
            maxzoom: 24 
          }
        },
        layers: [
          {
            id: 'background',
            type: 'background',
            paint: { 'background-color': '#0b0f19' }
          },
          {
            id: 'landcover-layer',
            type: 'fill',
            source: 'osm-vector',
            'source-layer': 'landcover',
            paint: { 'fill-color': '#111927', 'fill-opacity': 0.8 }
          },
          {
            id: 'landuse-layer',
            type: 'fill',
            source: 'osm-vector',
            'source-layer': 'landuse',
            paint: { 
              'fill-color': [
                'match', ['get', 'class'],
                'industrial', '#1f1d15',
                'commercial', '#162235',
                'residential', '#121824',
                'park', '#102218',
                '#131b2e'
              ], 
              'fill-opacity': 0.7 
            }
          },
          {
            id: 'water-layer',
            type: 'fill',
            source: 'osm-vector',
            'source-layer': 'water',
            paint: { 'fill-color': '#1b2a4a' }
          },
          {
            id: 'waterway-layer',
            type: 'line',
            source: 'osm-vector',
            'source-layer': 'waterway',
            paint: {
              'line-color': '#284b7a',
              'line-width': ['interpolate', ['linear'], ['zoom'], 10, 1, 16, 3]
            }
          },
          {
            id: 'building-layer',
            type: 'fill',
            source: 'osm-vector',
            'source-layer': 'building',
            minzoom: 14,
            paint: {
              'fill-color': '#1e293b',
              'fill-outline-color': '#334155'
            }
          },
          {
            id: 'boundary-layer',
            type: 'line',
            source: 'osm-vector',
            'source-layer': 'boundary',
            paint: {
              'line-color': '#475569',
              'line-width': ['interpolate', ['linear'], ['zoom'], 4, 1, 12, 3],
              'line-dasharray': [4, 2]
            }
          },
          {
            id: 'country-labels',
            type: 'symbol',
            source: 'osm-vector',
            'source-layer': 'place',
            minzoom: 2,
            maxzoom: 9,
            filter: ['==', 'class', 'country'],
            layout: {
              'text-field': ['coalesce', ['get', 'name:pt'], ['get', 'name']],
              'text-font': ['Noto Sans Regular'],
              'text-size': ['interpolate', ['linear'], ['zoom'], 2, 10, 8, 16],
              'text-transform': 'uppercase',
              'text-letter-spacing': 0.1
            },
            paint: {
              'text-color': '#64748b',
              'text-halo-color': '#0f172a',
              'text-halo-width': 2
            }
          },
          {
            id: 'state-labels',
            type: 'symbol',
            source: 'osm-vector',
            'source-layer': 'place',
            minzoom: 4,
            maxzoom: 12,
            filter: ['==', 'class', 'state'],
            layout: {
              'text-field': ['coalesce', ['get', 'name:pt'], ['get', 'name']],
              'text-font': ['Noto Sans Regular'],
              'text-size': ['interpolate', ['linear'], ['zoom'], 4, 10, 10, 14],
              'text-transform': 'uppercase'
            },
            paint: {
              'text-color': '#475569',
              'text-halo-color': '#0f172a',
              'text-halo-width': 1.5
            }
          },
          {
            id: 'city-labels',
            type: 'symbol',
            source: 'osm-vector',
            'source-layer': 'place',
            minzoom: 6,
            maxzoom: 16,
            layout: {
              'text-field': ['coalesce', ['get', 'name:pt'], ['get', 'name']], 
              'text-font': ['Noto Sans Regular'], 
              'text-size': ['interpolate', ['linear'], ['zoom'], 6, 10, 14, 16],
              'text-anchor': 'center',
              'text-allow-overlap': false
            },
            paint: {
              'text-color': '#94a3b8',
              'text-halo-color': '#0f172a',
              'text-halo-width': 2
            }
          },
          {
            id: 'road-minor',
            type: 'line',
            source: 'osm-vector',
            'source-layer': 'transportation',
            filter: ['in', 'class', 'minor', 'service', 'track'],
            paint: {
              'line-color': '#2b3748',
              'line-width': ['interpolate', ['linear'], ['zoom'], 12, 1, 18, 4]
            }
          },
          {
            id: 'road-secondary',
            type: 'line',
            source: 'osm-vector',
            'source-layer': 'transportation',
            filter: ['in', 'class', 'secondary', 'tertiary', 'link'],
            paint: {
              'line-color': '#3b4d66',
              'line-width': ['interpolate', ['linear'], ['zoom'], 10, 1.5, 18, 6]
            }
          },
          {
            id: 'road-major',
            type: 'line',
            source: 'osm-vector',
            'source-layer': 'transportation',
            filter: ['in', 'class', 'motorway', 'trunk', 'primary'],
            paint: {
              'line-color': '#475569',
              'line-width': ['interpolate', ['linear'], ['zoom'], 6, 2, 18, 10]
            }
          },
          {
            id: 'poi-labels',
            type: 'symbol',
            source: 'osm-vector',
            'source-layer': 'poi',
            minzoom: 15,
            layout: {
              'text-field': ['coalesce', ['get', 'name:pt'], ['get', 'name']],
              'text-font': ['Noto Sans Regular'],
              'text-size': 10,
              'text-offset': [0, 1],
              'text-anchor': 'top'
            },
            paint: {
              'text-color': '#38bdf8',
              'text-halo-color': '#0f172a',
              'text-halo-width': 1.5
            }
          },
          {
            id: 'road-labels',
            type: 'symbol',
            source: 'osm-vector',
            'source-layer': 'transportation_name',
            minzoom: 12,
            layout: {
              'text-field': ['coalesce', ['get', 'name:pt'], ['get', 'name'], ['get', 'ref']], 
              'text-font': ['Noto Sans Regular'], 
              'text-size': 11,
              'symbol-placement': 'line',
              'symbol-spacing': 150, 
              'text-allow-overlap': true, 
              'text-ignore-placement': true
            },
            paint: {
              'text-color': '#cbd5e1',
              'text-halo-color': '#0f172a',
              'text-halo-width': 1.5
            }
          },
          {
            id: 'tactical-master-layer',
            type: 'raster',
            source: 'tactical-master-source',
            paint: {
              'raster-opacity': 0.95
            }
          }
        ]
      },
      center: [-46.6333, -23.5505],
      zoom: 13,
      maxZoom: 24
    });

    // ATIVA O GLOBO NATIVO QUANDO O ESTILO FOR CARREGADO (Requer MapLibre v5+)
    map.on('style.load', () => {
      map.setProjection({ type: 'globe' });
    });

    const contextMenu = document.getElementById('context-menu');
    const menuOrigemBtn = document.getElementById('menu-rota-origem');
    const menuDestinoBtn = document.getElementById('menu-rota-destino');
    const hudStatus = document.getElementById('hud-status');

    const uploadModal = document.getElementById('upload-modal');
    const modalLat = document.getElementById('modal-lat');
    const modalLng = document.getElementById('modal-lng');
    const modalLayer = document.getElementById('modal-layer');
    const fileInput = document.getElementById('file-input');

    let menuLngLat = null;
    let menuPoint = null;
    let pontoOrigemRota = null;
    let marcadorOrigemRota = null;

    function calcularDistancia(lon1, lat1, lon2, lat2) {
      const R = 6371;
      const dLat = (lat2 - lat1) * Math.PI / 180;
      const dLon = (lon2 - lon1) * Math.PI / 180;
      const a = 
        Math.sin(dLat/2) * Math.sin(dLat/2) +
        Math.cos(lat1 * Math.PI / 180) * Math.cos(lat2 * Math.PI / 180) * 
        Math.sin(dLon/2) * Math.sin(dLon/2);
      const c = 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1-a));
      return R * c;
    }

    map.on('load', () => {
      const initialEl = document.createElement('div');
      initialEl.className = 'tactical-marker-container';
      initialEl.innerHTML = `
        <div class="tactical-dot"></div>
        <div class="tactical-label-box">ESTAÇÃO LOCAL (SÃO PAULO)</div>
      `;

      new maplibregl.Marker({ element: initialEl, anchor: 'left' })
        .setLngLat([-46.6333, -23.5505])
        .addTo(map);

      map.on('contextmenu', (e) => {
        e.preventDefault();
        menuLngLat = e.lngLat;
        menuPoint = e.point;

        if (!pontoOrigemRota) {
          menuOrigemBtn.style.display = 'block';
          menuDestinoBtn.style.display = 'none';
        } else {
          menuOrigemBtn.style.display = 'none';
          menuDestinoBtn.style.display = 'block';
        }

        contextMenu.style.display = 'block';
        contextMenu.style.left = `${e.originalEvent.clientX}px`;
        contextMenu.style.top = `${e.originalEvent.clientY}px`;
      });

      map.on('click', () => {
        contextMenu.style.display = 'none';
      });
    });

    document.getElementById('menu-marcador').addEventListener('click', () => {
      contextMenu.style.display = 'none';
      if (!menuLngLat) return;

      const features = map.queryRenderedFeatures(menuPoint, {
        layers: ['road-major', 'road-secondary', 'road-minor', 'road-labels']
      });

      let nomeRua = "PONTO ALVO";
      for (const f of features) {
        const props = f.properties;
        const textoEncontrado = props['name:pt'] || props['name'] || props['ref'];
        if (textoEncontrado) {
          nomeRua = textoEncontrado;
          break;
        }
      }

      let textoAlvo = prompt("Identificação do Ponto Tático:", nomeRua);
      if (!textoAlvo) return;

      const dynamicEl = document.createElement('div');
      dynamicEl.className = 'tactical-marker-container';
      dynamicEl.innerHTML = `
        <div class="click-dot"></div>
        <div class="click-label-box">${textoAlvo}</div>
      `;

      new maplibregl.Marker({ element: dynamicEl, anchor: 'left' })
        .setLngLat([menuLngLat.lng, menuLngLat.lat])
        .addTo(map);
    });

    menuOrigemBtn.addEventListener('click', () => {
      contextMenu.style.display = 'none';
      if (!menuLngLat) return;

      pontoOrigemRota = [menuLngLat.lng, menuLngLat.lat];

      const origEl = document.createElement('div');
      origEl.className = 'tactical-marker-container';
      origEl.innerHTML = `<div class="click-dot"></div><div class="click-label-box">ORIGEM DA ROTA</div>`;
      
      marcadorOrigemRota = new maplibregl.Marker({ element: origEl, anchor: 'left' })
        .setLngLat(pontoOrigemRota)
        .addTo(map);

      hudStatus.innerText = "[AGUARDANDO DESTINO DA ROTA]";
    });

    menuDestinoBtn.addEventListener('click', () => {
      contextMenu.style.display = 'none';
      if (!menuLngLat || !pontoOrigemRota) return;

      const pontoDestino = [menuLngLat.lng, menuLngLat.lat];
      const distanciaKm = calcularDistancia(pontoOrigemRota[0], pontoOrigemRota[1], pontoDestino[0], pontoDestino[1]);
      const distanciaTexto = distanciaKm < 1 ? `${(distanciaKm * 1000).toFixed(0)}m` : `${distanciaKm.toFixed(2)}km`;

      const destEl = document.createElement('div');
      destEl.className = 'tactical-marker-container';
      destEl.innerHTML = `<div class="click-dot" style="background-color: #ff0055; box-shadow: 0 0 10px #ff0055;"></div><div class="click-label-box" style="border-color: #ff0055;">DESTINO (${distanciaTexto})</div>`;
      
      new maplibregl.Marker({ element: destEl, anchor: 'left' })
        .setLngLat(pontoDestino)
        .addTo(map);

      const linhaGeoJSON = {
        type: 'Feature',
        properties: {},
        geometry: {
          type: 'LineString',
          coordinates: [pontoOrigemRota, pontoDestino]
        }
      };

      const layerId = 'rota-tatica-' + Date.now();
      const sourceId = 'fonte-rota-' + Date.now();

      map.addSource(sourceId, {
        type: 'geojson',
        data: linhaGeoJSON
      });

      map.addLayer({
        id: layerId,
        type: 'line',
        source: sourceId,
        layout: {
          'line-join': 'round',
          'line-cap': 'round'
        },
        paint: {
          'line-color': '#00ffff',
          'line-width': 3,
          'line-dasharray': [2, 2],
          'line-opacity': 0.9
        }
      });

      pontoOrigemRota = null;
      if (marcadorOrigemRota) marcadorOrigemRota.remove();
      hudStatus.innerText = "[CLIQUE DIREITO NO MAPA]";
    });

    document.getElementById('menu-info').addEventListener('click', () => {
      contextMenu.style.display = 'none';
      if (!menuPoint) return;

      const features = map.queryRenderedFeatures(menuPoint, {
        layers: ['road-major', 'road-secondary', 'road-minor', 'road-labels']
      });

      if (features.length > 0) {
        const props = features[0].properties;
        const jsonStr = JSON.stringify(props, null, 2);
        alert("Propriedades da Via (JSON):\n\n" + jsonStr);
      } else {
        alert("Nenhuma via detectada neste ponto exato.");
      }
    });

    document.getElementById('menu-upload').addEventListener('click', () => {
      contextMenu.style.display = 'none';
      if (!menuLngLat || !menuPoint) return;

      const features = map.queryRenderedFeatures(menuPoint);
      let camadaAtiva = "CAMADA BASE / TERRENO";
      let nomeDetalhe = "";

      if (features.length > 0) {
        camadaAtiva = features[0].layer.id.toUpperCase();
        const props = features[0].properties;
        nomeDetalhe = props['name:pt'] || props['name'] || props['ref'] || "";
      }

      modalLat.innerText = menuLngLat.lat.toFixed(6);
      modalLng.innerText = menuLngLat.lng.toFixed(6);
      modalLayer.innerText = nomeDetalhe ? `${camadaAtiva} (${nomeDetalhe.toUpperCase()})` : camadaAtiva;

      uploadModal.style.display = 'flex';
    });

    document.getElementById('upload-close').addEventListener('click', () => {
      uploadModal.style.display = 'none';
      fileInput.value = '';
    });

    document.getElementById('upload-submit').addEventListener('click', () => {
      const file = fileInput.files[0];
      if (!file) {
        alert("ERRO: NENHUM ARQUIVO DE IMAGEM SELECIONADO.");
        return;
      }

      const latSalva = menuLngLat.lat;
      const lngSalva = menuLngLat.lng;
      const zoomAtualUpload = map.getZoom();
      const nomeCamadaAlvo = modalLayer.innerText;

      const formData = new FormData();
      formData.append('imagem', file);
      formData.append('latitude', latSalva);
      formData.append('longitude', lngSalva);
      formData.append('zoom', zoomAtualUpload); 
      formData.append('camada', nomeCamadaAlvo);

      fetch('/api/tactical/upload', {
          method: 'POST',
          body: formData
      })
      .then(res => res.json())
      .then(data => {
          if (data.success) {
              alert(`[SUCESSO] Alvo incorporado ao Banco Mestre!\nDê um zoom para cima para revelar a planta.`);
              uploadModal.style.display = 'none';
              fileInput.value = '';

              const targetMarkerEl = document.createElement('div');
              targetMarkerEl.className = 'tactical-marker-container';
              targetMarkerEl.innerHTML = `
                <div class="click-dot" style="background-color: #ffaa00; box-shadow: 0 0 10px #ffaa00;"></div>
                <div class="click-label-box" style="border-color: #ffaa00;">ALVO: ${data.layer}</div>
              `;

              new maplibregl.Marker({ element: targetMarkerEl, anchor: 'left' })
                .setLngLat([lngSalva, latSalva])
                .addTo(map);

              // Força a atualização do Tile Cache ao forçar o redesenho do mapa
              if (map.getLayer('tactical-master-layer')) {
                  map.removeLayer('tactical-master-layer');
                  map.removeSource('tactical-master-source');
                  
                  map.addSource('tactical-master-source', {
                    type: 'raster',
                    tiles: [baseUrl + '/api/tactical/tiles/{z}/{x}/{y}?t=' + Date.now()],
                    tileSize: 256,
                    minzoom: 0,
                    maxzoom: 24
                  });
                  
                  map.addLayer({
                    id: 'tactical-master-layer',
                    type: 'raster',
                    source: 'tactical-master-source',
                    paint: { 'raster-opacity': 0.95 }
                  });
              }

              map.flyTo({
                center: [lngSalva, latSalva],
                zoom: zoomAtualUpload, 
                essential: true
              });

          } else {
              alert("ERRO: O servidor retornou uma falha no processamento.");
          }
      })
      .catch(error => {
          console.error('Erro na requisição:', error);
          alert("FALHA DE COMUNICAÇÃO COM O SERVIDOR TÁTICO.");
      });
    });

    function executarBuscaRua() {
      const termo = document.getElementById('search-input').value.trim().toLowerCase();
      if (!termo) return;

      const featuresTrans = map.querySourceFeatures('osm-vector', { sourceLayer: 'transportation' });
      const featuresNames = map.querySourceFeatures('osm-vector', { sourceLayer: 'transportation_name' });
      const allFeatures = [...featuresTrans, ...featuresNames];

      let ruaEncontrada = null;
      for (const f of allFeatures) {
        const props = f.properties;
        const nome = props['name:pt'] || props['name'] || props['ref'] || "";
        if (nome.toLowerCase().includes(termo)) {
          ruaEncontrada = f;
          break;
        }
      }

      if (ruaEncontrada && ruaEncontrada.geometry) {
        let coordsTarget = null;
        if (ruaEncontrada.geometry.type === 'LineString') {
          const coords = ruaEncontrada.geometry.coordinates;
          coordsTarget = coords[Math.floor(coords.length / 2)];
        } else if (ruaEncontrada.geometry.type === 'Point') {
          coordsTarget = ruaEncontrada.geometry.coordinates;
        }

        if (coordsTarget) {
          const nomeVia = ruaEncontrada.properties['name:pt'] || ruaEncontrada.properties.name || ruaEncontrada.properties.ref || 'ALVO LOCALIZADO';

          map.flyTo({ 
            center: coordsTarget, 
            zoom: 18, 
            essential: true 
          });

          const searchEl = document.createElement('div');
          searchEl.className = 'tactical-marker-container';
          searchEl.innerHTML = `
            <div class="click-dot" style="background-color: #dfa822; box-shadow: 0 0 10px #dfa822;"></div>
            <div class="click-label-box" style="border-color: #dfa822;">${nomeVia.toUpperCase()}</div>
          `;

          new maplibregl.Marker({ element: searchEl, anchor: 'left' })
            .setLngLat(coordsTarget)
            .addTo(map);
        }
      } else {
        alert("Nenhuma rua encontrada com esse termo na cache atual.");
      }
    }

    document.getElementById('search-btn').addEventListener('click', executarBuscaRua);
    document.getElementById('search-input').addEventListener('keydown', (e) => {
      if (e.key === 'Enter') executarBuscaRua();
    });
  </script>
</body>
</html>

```

### PASSO 5: Compilar o código no Raspberry PI 

1. Instalar as dependencias.
```bash
// Instala o pacote SqLite
go get modernc.org/sqlite

go get golang.org/x/image/draw
```
3. Acesse o diretório "/home/principal/mbtiles-server/" e execute o comando:
```bash
go build -ombtiles-server main.go
```
4. Se ocorrer a mensagem de erro "no such host", execute os comandos na sequencia:
```
// abre o arquivo de configuração com o nano
sudo nano /etc/resolv.conf

// Mova o cursor até o final do arquivo e adicione estas linhas (DNs do Google)
nameserver 8.8.8.8 
nameserver 1.1.1.1

Salve com `Ctrl + O`, `Enter` e saia com `Ctrl + X`
```
5. Tente compilar novamente
```bash
go build -o server_map main.go
```
6. Para executar chame
```bash
./server_map
```
7. Abra o navegador em http://alrischa:8080/


### PASSO 6: Iniciar automáticamente

1. Criar a pasta para o serviço do sistema
```
sudo nano /etc/systemd/system/mbtiles-server.service
```
2. Cole a configuração de serviço no arquivo server-map.service
```
[Unit]
Description=Servidor Map Tactical Core MBTiles
After=network.target

[Service]
Type=simple
User=principal
WorkingDirectory=/home/principal/mbtile-server
ExecStart=/home/principal/mbtile-server/mbtiles-server
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```
3. Reinicie o serviço do daemon
```
// Força a reinicialização
sudo systemctl daemon-reload 

// Cria o serviço vinculo do serviço com daemon
sudo systemctl enable mbtiles-server

// Inicia o serviço caso decida não reiniciar neste momento
sudo systemctl start mbtiles-server
```
##### Outros scripts úteis

Desativar o IPv6
```
// Tente executar esses comandos
sudo sysctl -w net.ipv6.conf.all.disable_ipv6=1 
sudo sysctl -w net.ipv6.conf.default.disable_ipv6=1 
sudo sysctl -w net.ipv6.conf.lo.disable_ipv6=1

// Se não der certo o comando acima acesse o arquivo de configuração
sudo nano /etc/sysctl.conf

// Adicione as seguintes linhas ao arquivo, isso desativa o IPV6 no Raspbian OS, salve e reinicie o Raspberry Pi
net.ipv6.conf.all.disable_ipv6 = 1 
net.ipv6.conf.default.disable_ipv6 = 1 
net.ipv6.conf.lo.disable_ipv6 = 1
```
