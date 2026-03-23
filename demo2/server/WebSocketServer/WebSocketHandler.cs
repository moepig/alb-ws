using System.Net.WebSockets;
using System.Text;

public record WebSocketHandlerOptions(TimeSpan? IdleTimeout);

public class WebSocketHandler(ILogger<WebSocketHandler> logger, WebSocketHandlerOptions options)
{
    public async Task HandleAsync(HttpContext ctx)
    {
        if (!ctx.WebSockets.IsWebSocketRequest)
        {
            ctx.Response.StatusCode = StatusCodes.Status400BadRequest;
            return;
        }

        var ws = await ctx.WebSockets.AcceptWebSocketAsync();
        var remote = ctx.Connection.RemoteIpAddress?.ToString() ?? "unknown";
        logger.LogInformation("[{Remote}] connected", remote);

        var buf = new byte[4096];

        try
        {
            while (ws.State == WebSocketState.Open)
            {
                WebSocketReceiveResult result;

                if (options.IdleTimeout is { } t)
                {
                    using var cts = new CancellationTokenSource(t);
                    try
                    {
                        result = await ws.ReceiveAsync(buf, cts.Token);
                    }
                    catch (OperationCanceledException)
                    {
                        logger.LogInformation("[{Remote}] idle timeout, closing", remote);
                        await ws.CloseAsync(WebSocketCloseStatus.NormalClosure, "idle timeout", CancellationToken.None);
                        return;
                    }
                }
                else
                {
                    result = await ws.ReceiveAsync(buf, CancellationToken.None);
                }

                if (result.MessageType == WebSocketMessageType.Close)
                {
                    logger.LogInformation("[{Remote}] disconnected", remote);
                    await ws.CloseAsync(WebSocketCloseStatus.NormalClosure, string.Empty, CancellationToken.None);
                    return;
                }

                var msg = Encoding.UTF8.GetString(buf, 0, result.Count);
                logger.LogInformation("[{Remote}] received type={Type} msg={Msg}", remote, result.MessageType, msg);
                await ws.SendAsync(new ArraySegment<byte>(buf, 0, result.Count), result.MessageType, result.EndOfMessage, CancellationToken.None);
            }
        }
        catch (WebSocketException ex)
        {
            logger.LogError("[{Remote}] error: {Message}", remote, ex.Message);
        }
    }
}
