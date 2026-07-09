#!/usr/bin/env node
// Mints a JWT with the same claims issueJWT() produces (sub, uid, name, role, exp),
// signed HS256 with JWT_SECRET — for local manual testing only, never for production.
// Usage: node scripts/mint-dev-jwt.js <user_id> <line_id> [role] [secret]
const crypto = require("node:crypto");

const [, , uid, lineId, role = "dealer", secret = process.env.JWT_SECRET || "dev-jwt-secret-change-me"] = process.argv;
if (!uid || !lineId) {
  console.error("Usage: node scripts/mint-dev-jwt.js <user_id> <line_id> [role] [secret]");
  process.exit(1);
}

function b64url(input) {
  return Buffer.from(input).toString("base64").replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

const header = { alg: "HS256", typ: "JWT" };
const payload = {
  sub: lineId,
  uid,
  name: "Test User",
  role,
  exp: Math.floor(Date.now() / 1000) + 7 * 24 * 3600,
};

const data = `${b64url(JSON.stringify(header))}.${b64url(JSON.stringify(payload))}`;
const sig = crypto.createHmac("sha256", secret).update(data).digest();
console.log(`${data}.${b64url(sig)}`);
