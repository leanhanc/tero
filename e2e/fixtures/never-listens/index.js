// Stays alive without ever listening on PORT, so the start check must fail.
console.log("working, but not listening on PORT");
setInterval(() => {}, 1000);
