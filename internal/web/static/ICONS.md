# App icons

The PNGs are rendered from `favicon.svg` (a red tile with a white glucose
wave) with `rsvg-convert` (librsvg). Regenerate them after changing the logo:

```sh
cd internal/web/static
rsvg-convert -w 192 -h 192 favicon.svg -o icon-192.png
rsvg-convert -w 512 -h 512 favicon.svg -o icon-512.png
```

`icon-maskable-512.png` and `apple-touch-icon.png` (180 px) come from a
full-bleed variant: the same red (`#e5484d`) filling the whole square, with the
wave scaled to 66 % and centred, so Android's and iOS's rounded masks never cut
into it:

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" fill="#e5484d"/><g transform="translate(16 16) scale(0.66) translate(-16 -16)"><path d="M4 20 L10 20 L13 11 L17 24 L20 15 L28 15" fill="none" stroke="#fff" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/></g></svg>
```

```sh
rsvg-convert -w 512 -h 512 maskable.svg -o icon-maskable-512.png
rsvg-convert -w 180 -h 180 maskable.svg -o apple-touch-icon.png
```

`badge-96.png` is the small monochrome icon Android shows in the status bar
for a push notification: the wave alone, white on transparent (stroke width
3.2, `viewBox="0 0 32 32"`), rendered at 96 px.

The manifest (`/manifest.webmanifest`, see `internal/web/pwa.go`) lists the
192, 512 and maskable icons.
