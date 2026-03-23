using System.Globalization;
using System.Net;
using System.Net.WebSockets;
using System.Text;
using Microsoft.AspNetCore.Http;
using Microsoft.AspNetCore.Http.HttpResults;
using Microsoft.AspNetCore.Mvc.Testing;
using Microsoft.Extensions.Logging.Abstractions;
using Xunit;

// ---- HealthHandler unit tests ----

public class HealthHandlerTests
{
    [Fact]
    public void Handle_ReturnsContentResult_WithOkBody()
    {
        var result = HealthHandler.Handle();

        var content = Assert.IsType<ContentHttpResult>(result);
        Assert.Equal("ok", content.ResponseContent);
    }

    [Fact]
    public void Handle_ReturnsContentResult_WithNullStatusCode()
    {
        // null means 200 OK (default)
        var content = Assert.IsType<ContentHttpResult>(HealthHandler.Handle());
        Assert.Null(content.StatusCode);
    }
}

// ---- WebSocketHandler unit tests ----

public class WebSocketHandlerTests
{
    [Fact]
    public async Task HandleAsync_NonWebSocketRequest_Sets_StatusCode400()
    {
        var handler = new WebSocketHandler(
            NullLogger<WebSocketHandler>.Instance,
            new WebSocketHandlerOptions(IdleTimeout: null));

        var ctx = new DefaultHttpContext();
        // DefaultHttpContext.WebSockets.IsWebSocketRequest is false

        await handler.HandleAsync(ctx);

        Assert.Equal(400, ctx.Response.StatusCode);
    }
}

// ---- Integration tests via WebApplicationFactory ----

public class ServerIntegrationTests(WebApplicationFactory<Program> factory)
    : IClassFixture<WebApplicationFactory<Program>>
{
    [Fact]
    public async Task HealthEndpoint_Returns_Ok()
    {
        var response = await factory.CreateClient().GetAsync("/health");

        response.EnsureSuccessStatusCode();
        Assert.Equal("ok", (await response.Content.ReadAsStringAsync()).Trim());
    }

    [Fact]
    public async Task WsEndpoint_NonWebSocketRequest_Returns_BadRequest()
    {
        var response = await factory.CreateClient().GetAsync("/ws");

        Assert.Equal(HttpStatusCode.BadRequest, response.StatusCode);
    }

    [Fact]
    public async Task WebSocket_TextMessage_IsEchoed()
    {
        var ws = await ConnectAsync();

        Assert.Equal("hello", await SendTextAndReceiveAsync(ws, "hello"));

        await ws.CloseAsync(WebSocketCloseStatus.NormalClosure, string.Empty, CancellationToken.None);
    }

    [Fact]
    public async Task WebSocket_BinaryMessage_IsEchoed()
    {
        var ws = await ConnectAsync();

        byte[] data = [0x01, 0x02, 0x03];
        await ws.SendAsync(data, WebSocketMessageType.Binary, endOfMessage: true, CancellationToken.None);

        var buf = new byte[64];
        using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(5));
        var result = await ws.ReceiveAsync(buf, cts.Token);

        Assert.Equal(WebSocketMessageType.Binary, result.MessageType);
        Assert.Equal(data, buf[..result.Count]);

        await ws.CloseAsync(WebSocketCloseStatus.NormalClosure, string.Empty, CancellationToken.None);
    }

    [Fact]
    public async Task WebSocket_MultipleMessages_AllEchoed()
    {
        var ws = await ConnectAsync();

        foreach (var msg in new[] { "first", "second", "third" })
            Assert.Equal(msg, await SendTextAndReceiveAsync(ws, msg));

        await ws.CloseAsync(WebSocketCloseStatus.NormalClosure, string.Empty, CancellationToken.None);
    }

    [Fact]
    public async Task WebSocket_NormalClose_HandledGracefully()
    {
        var ws = await ConnectAsync();

        await ws.CloseAsync(WebSocketCloseStatus.NormalClosure, string.Empty, CancellationToken.None);

        Assert.True(ws.State is WebSocketState.Closed or WebSocketState.CloseReceived or WebSocketState.CloseSent);
    }

    // ---- helpers ----

    private async Task<WebSocket> ConnectAsync()
    {
        var wsClient = factory.Server.CreateWebSocketClient();
        return await wsClient.ConnectAsync(new Uri("ws://localhost/ws"), CancellationToken.None);
    }

    private static async Task<string> SendTextAndReceiveAsync(WebSocket ws, string message)
    {
        await ws.SendAsync(Encoding.UTF8.GetBytes(message), WebSocketMessageType.Text, endOfMessage: true, CancellationToken.None);

        var buf = new byte[1024];
        using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(5));
        var result = await ws.ReceiveAsync(buf, cts.Token);
        return Encoding.UTF8.GetString(buf, 0, result.Count);
    }
}

// ---- Idle timeout tests ----

public class IdleTimeoutTests
{
    private static WebApplicationFactory<Program> CreateFactory(double idleTimeoutSecs) =>
        new WebApplicationFactory<Program>().WithWebHostBuilder(b =>
            b.UseSetting("idle-timeout", idleTimeoutSecs.ToString(CultureInfo.InvariantCulture))
             .UseSetting("keep-alive-interval", "0"));

    [Fact]
    public async Task IdleTimeout_ClosesConnection_WhenNoMessageReceived()
    {
        await using var factory = CreateFactory(0.5);
        var ws = await factory.Server.CreateWebSocketClient()
            .ConnectAsync(new Uri("ws://localhost/ws"), CancellationToken.None);

        var buf = new byte[64];
        using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(5));
        var result = await ws.ReceiveAsync(buf, cts.Token);

        Assert.Equal(WebSocketMessageType.Close, result.MessageType);
        Assert.Equal(WebSocketCloseStatus.NormalClosure, result.CloseStatus);
    }

    [Fact]
    public async Task IdleTimeout_NotReached_WhenMessageSentBeforeExpiry()
    {
        await using var factory = CreateFactory(0.5);
        var ws = await factory.Server.CreateWebSocketClient()
            .ConnectAsync(new Uri("ws://localhost/ws"), CancellationToken.None);

        // 送信タイミングをタイムアウト (500ms) より十分早くする
        await Task.Delay(100);
        await ws.SendAsync(Encoding.UTF8.GetBytes("hi"), WebSocketMessageType.Text, endOfMessage: true, CancellationToken.None);

        var buf = new byte[64];
        using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(5));
        var result = await ws.ReceiveAsync(buf, cts.Token);

        // Close ではなくエコーが返ってくるはず
        Assert.Equal(WebSocketMessageType.Text, result.MessageType);
        Assert.Equal("hi", Encoding.UTF8.GetString(buf, 0, result.Count));

        await ws.CloseAsync(WebSocketCloseStatus.NormalClosure, string.Empty, CancellationToken.None);
    }
}
