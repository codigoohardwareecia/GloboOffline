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

string mbtilesPath = @"C:\SeuMapa\mbtiles\2019-09-planet-14.mbtiles";
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
    string fontPath = Path.Combine(@"C:\SeuMapa\", "fonts", fontstack, $"{range}.pbf");

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