// Package pidev is the HTTP-neutral domain for the pi.dev structured
// model directory (https://pi.dev/api/models). It owns the restricted
// HTTPS client (timeout, redirects, ETag/304, singleflight, last-known-good),
// typed catalog revision/minimum-version validation, and the full-ID +
// Pi-API candidate matching contract for Prism's Pi-only export.
package pidev
