# Mapa - Passo a passo para criar um Globo com mapas Offline

#### Passo 1: Downloads necessários

Primeiramente vamos fazer o download dos arquivos que precisamos:

1. Download do arquivo MBTile completo
```
// Link de download com mapas dos continentes
https://archive.org/download/osm-vector-mbtiles/regions/
	
// America do Sul (nivel 14)
https://archive.org/download/osm-vector-mbtiles/regions/2019-09-north-america-14.mbtiles
	
// Planeta (Nivel 14
https://archive.org/download/osm-vector-mbtiles/2020-10-mexico-14.mbtiles)
	
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

#### Passo 2: Criar um projeto .NET Core Minimal API

 1. Criar e Acessar o Diretório do Projeto:  abra o terminal ou o Prompt de Comando (CMD/PowerShell) e execute os comandos: 

```bash
mkdir MapTacticalApi
cd MapTacticalApi
```
 2. Inicializar o Projeto Minimal API
	Gere a estrutura inicial da aplicação Web enxuta (.NET Core):
```bash
dotnet new web
```
  3. Instalar Pacotes NuGet Necessários:  para dar suporte ao **SQLite** e ao manipulação de **Imagens/GZip**, instale as seguintes dependências:
```bash
dotnet add package Microsoft.Data.Sqlite
dotnet add package System.Drawing.Common
``` 

#### Passo 3:  Código C Sharp#

1. Abra o arquivo `Program.cs` no seu editor e substitua todo o seu conteúdo pelo código corrigido e formatado abaixo:
 
```csharp
using Microsoft.Data.Sqlite;
using System.Drawing;
using System.Drawing.Imaging;
using System.IO.Compression;

var builder = WebApplication.CreateBuilder(args);

builder.Services.AddCors(options =>
{
    options.AddDefaultPolicy(policy =>
    {
        policy.AllowAnyOrigin().AllowAnyMethod().AllowAnyHeader();
    });
});

var app = builder.Build();

app.UseCors();
app.UseDefaultFiles();
app.UseStaticFiles();

string mbtilesPath = @"D:\Mapa\2019-10-south-america-14.mbtiles";
string pastaMbTilesMaster = Path.Combine(AppContext.BaseDirectory, "MasterMbTiles");
Directory.CreateDirectory(pastaMbTilesMaster);
string caminhoMasterMbTiles = Path.Combine(pastaMbTilesMaster, "master_tactical.mbtiles");

InicializarBancoMestreUnificado(caminhoMasterMbTiles);

app.MapGet("/data/v3/{z}/{x}/{y}.pbf", async (int z, int x, int y, HttpContext context) =>
{
    int invertedY = (int)(Math.Pow(2, z) - 1 - y);
    string connString = $"Data Source={mbtilesPath};Mode=ReadOnly;";

    using var connection = new SqliteConnection(connString);
    await connection.OpenAsync();

    string query = "SELECT tile_data FROM tiles WHERE zoom_level = @z AND tile_column = @x AND tile_row = @y";
    using var command = new SqliteCommand(query, connection);
    command.Parameters.AddWithValue("@z", z);
    command.Parameters.AddWithValue("@x", x);
    command.Parameters.AddWithValue("@y", invertedY);

    var result = await command.ExecuteScalarAsync();
    if (result is byte[] tileBytes)
    {
        byte[] finalBytes = tileBytes;

        if (tileBytes.Length > 2 && tileBytes[0] == 0x1f && tileBytes[1] == 0x8b)
        {
            using var compressedStream = new MemoryStream(tileBytes);
            using var gzipStream = new GZipStream(compressedStream, CompressionMode.Decompress);
            using var resultStream = new MemoryStream();
            gzipStream.CopyTo(resultStream);
            finalBytes = resultStream.ToArray();
        }

        context.Response.Headers["Content-Encoding"] = "identity";
        return Results.File(finalBytes, "application/x-protobuf");
    }
    return Results.NotFound();
});

app.MapGet("/fonts/{fontstack}/{range}.pbf", async (string fontstack, string range, HttpContext context) =>
{
    fontstack = Uri.UnescapeDataString(fontstack);
    string fontPath = Path.Combine(AppContext.BaseDirectory, "fonts", fontstack, $"{range}.pbf");

    if (File.Exists(fontPath))
    {
        var bytes = await File.ReadAllBytesAsync(fontPath);
        context.Response.Headers["Content-Encoding"] = "identity";
        return Results.File(bytes, "application/x-protobuf");
    }
    return Results.NotFound();
});

// Endpoint de Upload: Adiciona a nova camada mantendo as anteriores intactas no banco mestre
app.MapPost("/api/tactical/upload", async (HttpRequest request) =>
{
    if (!request.HasFormContentType)
    {
        return Results.BadRequest(new { error = "O formato da requisição deve ser multipart/form-data." });
    }

    var form = await request.ReadFormAsync();

    string latStr = form["latitude"];
    string lonStr = form["longitude"];
    string zoomStr = form["zoom"];
    string camada = form["camada"];

    var file = form.Files["imagem"];
    if (file == null || file.Length == 0)
    {
        return Results.BadRequest(new { error = "Nenhum arquivo de imagem válido foi enviado." });
    }

    if (!double.TryParse(latStr, System.Globalization.NumberStyles.Any, System.Globalization.CultureInfo.InvariantCulture, out double latitude) ||
        !double.TryParse(lonStr, System.Globalization.NumberStyles.Any, System.Globalization.CultureInfo.InvariantCulture, out double longitude) ||
        !double.TryParse(zoomStr, System.Globalization.NumberStyles.Any, System.Globalization.CultureInfo.InvariantCulture, out double rawZoom))
    {
        return Results.BadRequest(new { error = "Parâmetros de coordenadas ou zoom inválidos." });
    }

    int targetZoom = (int)Math.Round(rawZoom) + 1;
    if (targetZoom < 0) targetZoom = 0;
    if (targetZoom > 30) targetZoom = 30;

    string pastaUploads = Path.Combine(AppContext.BaseDirectory, "UploadsTactical");
    Directory.CreateDirectory(pastaUploads);

    string nomeArquivoUnico = $"{DateTime.Now:yyyyMMdd_HHmmss}_{file.FileName}";
    string caminhoImagem = Path.Combine(pastaUploads, nomeArquivoUnico);

    using (var stream = new FileStream(caminhoImagem, FileMode.Create))
    {
        await file.CopyToAsync(stream);
    }

    try
    {
        AdicionarTileAoBancoMestreAcumulativo(caminhoImagem, caminhoMasterMbTiles, latitude, longitude, targetZoom);
        Console.WriteLine($"[MBTILES MESTRE] Nova camada acumulada com sucesso no zoom {targetZoom}!");
    }
    catch (Exception ex)
    {
        Console.WriteLine($"[ERRO MBTILES MESTRE] Falha: {ex.Message}");
        return Results.StatusCode(500);
    }

    return Results.Ok(new
    {
        success = true,
        message = "Camada tática acumulada com sucesso no banco mestre!",
        coordinates = new { latitude, longitude },
        zoom = targetZoom,
        layer = camada
    });
});

app.MapGet("/api/tactical/tiles/{z}/{x}/{y}", async (int z, int x, int y) =>
{
    if (!File.Exists(caminhoMasterMbTiles)) return Results.NotFound();

    int invertedY = (int)(Math.Pow(2, z) - 1 - y);
    string connString = $"Data Source={caminhoMasterMbTiles};Mode=ReadOnly;";

    using var connection = new SqliteConnection(connString);
    await connection.OpenAsync();

    string query = "SELECT tile_data FROM tiles WHERE zoom_level = @z AND tile_column = @x AND tile_row = @y";
    using var command = new SqliteCommand(query, connection);
    command.Parameters.AddWithValue("@z", z);
    command.Parameters.AddWithValue("@x", x);
    command.Parameters.AddWithValue("@y", invertedY);

    var result = await command.ExecuteScalarAsync();
    if (result is byte[] tileBytes)
    {
        return Results.File(tileBytes, "image/png");
    }

    return Results.NotFound();
});

app.Run("http://localhost:8080");

void InicializarBancoMestreUnificado(string caminhoDb)
{
    if (!File.Exists(caminhoDb))
    {
        using var connection = new SqliteConnection($"Data Source={caminhoDb}");
        connection.Open();
        string sql = @"
            CREATE TABLE metadata (name TEXT, value TEXT);
            CREATE TABLE tiles (zoom_level INTEGER, tile_column INTEGER, tile_row INTEGER, tile_data BLOB);
            CREATE UNIQUE INDEX tile_index on tiles (zoom_level, tile_column, tile_row);
            
            INSERT INTO metadata (name, value) VALUES ('name', 'Mapa Offline');
            INSERT INTO metadata (name, value) VALUES ('format', 'png');
            INSERT INTO metadata (name, value) VALUES ('bounds', '-180,-85.0511,180,85.0511');
            INSERT INTO metadata (name, value) VALUES ('center', '0,0,0');
            INSERT INTO metadata (name, value) VALUES ('minzoom', '0');
            INSERT INTO metadata (name, value) VALUES ('maxzoom', '30');
            INSERT INTO metadata (name, value) VALUES ('type', 'overlay');
        ";
        using var cmd = new SqliteCommand(sql, connection);
        cmd.ExecuteNonQuery();
    }
}

void AdicionarTileAoBancoMestreAcumulativo(string caminhoImagem, string caminhoDb, double lat, double lon, int zoom)
{
    using var connection = new SqliteConnection($"Data Source={caminhoDb}");
    connection.Open();

    long tileX = LongitudeToTileX(lon, zoom);
    long tileY = LatitudeToTileY(lat, zoom);

    using var imagemOriginal = new Bitmap(caminhoImagem);
    using var tileBitmap = new Bitmap(256, 256);
    using (var g = Graphics.FromImage(tileBitmap))
    {
        g.Clear(Color.Transparent);
        g.InterpolationMode = System.Drawing.Drawing2D.InterpolationMode.HighQualityBicubic;
        g.PixelOffsetMode = System.Drawing.Drawing2D.PixelOffsetMode.HighQuality;
        g.SmoothingMode = System.Drawing.Drawing2D.SmoothingMode.HighQuality;

        g.DrawImage(imagemOriginal, new Rectangle(0, 0, 256, 256));
    }

    byte[] tileBytes;
    using (var ms = new MemoryStream())
    {
        tileBitmap.Save(ms, ImageFormat.Png);
        tileBytes = ms.ToArray();
    }

    int maxTiles = (int)Math.Pow(2, zoom);
    int tileRowTms = maxTiles - 1 - (int)tileY;

    // INSERT OR IGNORE ou gerencia para não sobrescrever se for em coordenadas diferentes,
    // mas mantendo múltiplos registros de zooms diferentes intactos.
    string insertTileSql = "INSERT OR REPLACE INTO tiles (zoom_level, tile_column, tile_row, tile_data) VALUES (@z, @x, @y, @data);";
    using var insertCmd = new SqliteCommand(insertTileSql, connection);
    insertCmd.Parameters.AddWithValue("@z", zoom);
    insertCmd.Parameters.AddWithValue("@x", (int)tileX);
    insertCmd.Parameters.AddWithValue("@y", tileRowTms);
    insertCmd.Parameters.AddWithValue("@data", tileBytes);
    insertCmd.ExecuteNonQuery();
}

long LongitudeToTileX(double lon, int zoom)
{
    return (long)((lon + 180.0) / 360.0 * Math.Pow(2, zoom));
}

long LatitudeToTileY(double lat, int zoom)
{
    double latRad = lat * Math.PI / 180.0;
    return (long)((1.0 - Math.Log(Math.Tan(latRad) + 1.0 / Math.Cos(latRad)) / Math.PI) / 2.0 * Math.Pow(2, zoom));
}
```

2. Altere esta linha "string mbtilesPath = @"D:\Mapa\2019-10-south-america-14.mbtiles";" para o diretório e o nome do arquivo que você baixou no passo 1.

3. Executar o projeto: Execute o comando no terminal para compilar e iniciar a API

```bash
// Por linha de comando
dotnet run
```

A API estará disponível para receber requisições no endereço:
`http://localhost:8080`

#### Passo 4:  Código HTML

1. Crie um pasta chamada "wwwroot" na raiz do projeto para conter os arquivos estáticos, pode ser qualquer nome sugeri este por padrão. em um lugar de sua preferência:

2. Copie os arquivos  maplibre-gl.js e maplibre-gl.css que baixamos no passo 1 para dentro desta pasta

3. Abra um editor de texto e vamos criar um arquivo html com o seguinte conteúdo:

``` HTML
<!DOCTYPE html>
<html lang="pt-br">
<head>
  <meta charset="UTF-8">
  <title>Mapa Offline</title>
  
  <!-- MapLibre GL CSS Local (Versão 5+) -->
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
    SISTEMA: <span class="highlight">Mapa Offine</span><br>
    STATUS: <span class="status-live">ONLINE</span><br>
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
    const origin = window.location.origin;

    const map = new maplibregl.Map({
      container: 'map',
      style: {
        version: 8,
        glyphs: `${origin}/fonts/{fontstack}/{range}.pbf`,
        sources: {
          'osm-vector': {
            type: 'vector',
            tiles: [`${origin}/data/v3/{z}/{x}/{y}.pbf`],
            minzoom: 0,
            maxzoom: 14 
          },
          'tactical-master-source': {
            type: 'raster',
            tiles: [`${origin}/api/tactical/tiles/{z}/{x}/{y}`],
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
      center: [-46.63, -23.55],
      zoom: 15,
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
        <div class="tactical-label-box">SEU LOCAL</div>
      `;

      new maplibregl.Marker({ element: initialEl, anchor: 'left' })
        .setLngLat([-47.15, -22.76])
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

      fetch(`${origin}/api/tactical/upload`, {
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

              if (map.getLayer('tactical-master-layer')) {
                  map.removeLayer('tactical-master-layer');
                  map.removeSource('tactical-master-source');
                  
                  map.addSource('tactical-master-source', {
                    type: 'raster',
                    tiles: [`${origin}/api/tactical/tiles/{z}/{x}/{y}?t=` + Date.now()],
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

3. Salve como "index.html"
4. Abra o arquivo com duplo clique, é para abrir seu navegador, e carregar o mapa
