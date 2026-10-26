# Troubleshooting

Start with `srv doctor`: it checks every moving part and prints the fix
for what it finds. See [Doctor](daemon.md#doctor) for the full list.

## The certificate is not trusted

Restart the browser after adding the first local site. The CA is
installed system-wide, but browsers only pick it up on restart.

## The site is not reachable

```sh
srv doctor
srv logs mysite
```

## The hostname does not resolve

`srv doctor` reports whether srv's DNS server is running and whether the
system resolver routes through it. `srv install` runs the resolver setup
again.

## A port is already in use

```sh
srv doctor
sudo lsof -i :80
sudo lsof -i :443
```

## Starting over

```sh
srv install --fresh --yes   # remove all configuration and set up again
srv uninstall               # remove srv from the system entirely
```
