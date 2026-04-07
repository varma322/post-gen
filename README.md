# Affiliate Content Generation Engine (`postgen`)

A high-performance, CLI-driven tool to automate the creation of affiliate product posts from e-commerce product pages (Amazon and more) using custom templates.

## 🏗️ Architecture

- **Modular Scraper**: Decoupled interface-based design to support multiple platforms (Amazon, Flipkart, etc.).
- **Template Engine**: Uses Go's `text/template` for logic-heavy, beautiful post-generation. Includes **built-in caching** for high performance during bulk runs.
- **Fail-Safe Processing**: Retries and random delays are implemented to minimize blocking.
- **Multi-Output System**: Supports both console and file-based outputs with configurable modes.

## 🚀 Getting Started

Ensure the folder contains the following assets:
- `postgen.exe`: The binary.
- `accounts.json`: Maps account names to template files.
- `selectors.json`: Multi-platform CSS selectors for scraping.
- `/templates`: Folder with `.tmpl` files.
- `/output`: Folder where generated posts are stored.

### 🛠️ CLI Usage

#### Single URL Processing
Generate a post for a specific account:
```powershell
.\postgen.exe --url "https://amzn.in/example" --account afficart
```

Generate for **all** registered accounts:
```powershell
.\postgen.exe --url "https://amzn.in/example" --all
```

#### 📦 Bulk Processing
Process a list of URLs from a text file (one URL per line):
```powershell
.\postgen.exe --file links.txt --all
```

#### ⚙️ Advanced Flags
- `--split`: In bulk mode, saves each product to its own file (e.g. `afficart_logitech_mouse.txt`) instead of appending.
- `--clear`: Wipes the `/output` folder before starting the run. Perfect for fresh batches.

## 📄 Output Modes

1. **Append Mode (Default)**: All posts for a specific account are bundled into a single file (e.g. `output/afficart.txt`), separated by `-------------------`. Ideal for easy copy-pasting.
2. **Split Mode (`--split`)**: Each product gets its own file. Filenames are automatically sanitized and truncated (max 20 chars) from the product title.

## 🎨 Adding Templates

1. Create a `.tmpl` file in the `/templates` folder.
2. Register it in `accounts.json`:
   ```json
   {
      "name": "my_new_group",
      "template_path": "templates/new_style.tmpl"
   }
   ```

## 🔍 Selectors configuration

Amazon's DOM changes frequently. If the scraper stops pulling data, update the selectors in `selectors.json`. You can add selectors for new platforms in the same file:
```json
{
  "amazon": {
    "title": "#productTitle",
    "price": "...",
    "mrp": "...",
    "features": "..."
  },
  "flipkart": { ... }
}
```

## 📜 Logging

The tool logs real-time status to the console with `[INFO]` and `[ERR]` prefixes. For bulk runs, it includes a progress indicator such as `[4/20]`.
