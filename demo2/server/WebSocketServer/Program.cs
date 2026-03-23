using System.Globalization;

var builder = WebApplication.CreateBuilder(args);

// ---- configuration ----
// CLI: --keep-alive-interval 50 --keep-alive-timeout 10 --idle-timeout 10 --addr http://0.0.0.0:8080
// Tests: WithWebHostBuilder(b => b.UseSetting("idle-timeout", "0.5"))
double keepAliveIntervalSecs = double.Parse(builder.Configuration["keep-alive-interval"] ?? "30", CultureInfo.InvariantCulture);
double keepAliveTimeoutSecs  = double.Parse(builder.Configuration["keep-alive-timeout"]  ?? "10", CultureInfo.InvariantCulture);
double idleTimeoutSecs       = double.Parse(builder.Configuration["idle-timeout"]        ?? "0",  CultureInfo.InvariantCulture);
string addr                  = builder.Configuration["addr"] ?? "http://0.0.0.0:8080";

builder.WebHost.UseUrls(addr);
builder.Logging.ClearProviders();
builder.Logging.AddSimpleConsole(o => o.TimestampFormat = "yyyy/MM/dd HH:mm:ss ");

var idleTimeout = idleTimeoutSecs > 0 ? (TimeSpan?)TimeSpan.FromSeconds(idleTimeoutSecs) : null;
builder.Services.AddSingleton(new WebSocketHandlerOptions(idleTimeout));
builder.Services.AddSingleton<WebSocketHandler>();

var app = builder.Build();

var keepAliveInterval = keepAliveIntervalSecs > 0
    ? TimeSpan.FromSeconds(keepAliveIntervalSecs)
    : Timeout.InfiniteTimeSpan;
var keepAliveTimeout = keepAliveIntervalSecs > 0 && keepAliveTimeoutSecs > 0
    ? TimeSpan.FromSeconds(keepAliveTimeoutSecs)
    : Timeout.InfiniteTimeSpan;

app.UseWebSockets(new WebSocketOptions
{
    KeepAliveInterval = keepAliveInterval,
    KeepAliveTimeout  = keepAliveTimeout,
});

if (keepAliveIntervalSecs > 0)
    app.Logger.LogInformation(
        "server listening on {Addr} (keep-alive-interval={Interval}s, keep-alive-timeout={Timeout}s, idle-timeout={IdleTimeout}s)",
        addr, keepAliveIntervalSecs, keepAliveTimeoutSecs, idleTimeoutSecs);
else
    app.Logger.LogInformation(
        "server listening on {Addr} (keep-alive-interval=disabled, idle-timeout={IdleTimeout}s)",
        addr, idleTimeoutSecs);

app.MapGet("/health", HealthHandler.Handle);
app.Map("/ws", (HttpContext ctx, WebSocketHandler handler) => handler.HandleAsync(ctx));

app.Run();

// Exposed for WebApplicationFactory in tests
public partial class Program { }
