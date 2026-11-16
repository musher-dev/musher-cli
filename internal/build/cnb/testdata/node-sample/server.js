const http = require("http");

http
  .createServer((_req, res) => res.end("ok\n"))
  .listen(process.env.PORT || 8080);
