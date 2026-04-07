# Affiliate Content Generation Engine (`postgen`)

A CLI-first tool to automate compiling Amazon product data into beautiful, template-driven affiliate posts.

## Architecture
- **Scraper**: Pulls core data directly from Amazon product pages (Title, Price, Features).
- **Template Engine**: Processes `text/template` formats with logic loops and variables.
- **Output System**: Generates perfectly formatted posts both to standard output and discrete files in `/output`.

## Getting Started

1. Ensure the binary `postgen.exe` is in your execution path, or run it directly.
2. The folder must contain:
    - `accounts.json`: Configuration for which template to use for which account.
    - `selectors.json`: DOM selectors for scraping.
    - `/templates`: Folder containing all `.tmpl` files.
    - `/output`: Folder to which final texts are saved.

### Usage
Generate a post for a specific account:
```powershell
.\postgen.exe --url "https://amzn.to/example" --account afficart
```

Generate posts for **all** accounts simultaneously:
```powershell
.\postgen.exe --url "https://amzn.to/example" --all
```

## Adding Templates
1. Add `myformat.tmpl` to the `/templates` folder.
2. Register it in `accounts.json`:
   ```json
   {
      "name": "myformat",
      "template_path": "templates/myformat.tmpl"
   }
   ```

## Selectors
Amazon DOM changes can be handled gracefully by updating `selectors.json`. Ensure the selectors accurately target the corresponding text without extra nested noisy tags if possible.
