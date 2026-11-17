---
title: Passerelle IA MCP & Sandbox de Confidentialité
description: Connectez Claude Code, Claude Desktop, Cursor ou Antigravity à OVH Nexus via le protocole MCP avec pseudonymisation bidirectionnelle et interdiction stricte du paiement automatique.
---

La **Passerelle IA MCP** (`/mcp`, propulsée par `internal/mcp`) expose un point de terminaison **Model Context Protocol (MCP) Streamable HTTP** intégré (`/api/mcp`, JSON-RPC 2.0), inspiré de l'ergonomie de l'[OVHcloud CLI officielle (`ovh/ovhcloud-cli`)](https://github.com/ovh/ovhcloud-cli).

Parce qu'OVH Nexus gère des actifs d'infrastructure réels, la passerelle applique un **Bouclier de Confidentialité Bidirectionnel** ainsi que **4 niveaux de permissions et garde-fous financiers** : l'IA peut inventorier votre flotte, vérifier les stocks mondiaux et préparer des bons de commande non payés sans jamais accéder aux identifiants sensibles ni déclencher de prélèvement automatique.

---

## Architecture de Sécurité

### 1. Jeton Bearer Isolé (`nxs_mcp_...`) & Désactivé par Défaut

- **Désactivé par défaut** : `/api/mcp` renvoie `403 Forbidden` tant que la passerelle n'est pas activée dans **WebUI → Passerelle IA MCP (`/mcp`)**.
- **Authentification isolée** : La passerelle utilise un jeton Bearer dédié à haute entropie (`Authorization: Bearer nxs_mcp_...`), stocké exclusivement sous forme d'empreinte `SHA-256` irréversible (`token_hash`) et d'aperçu masqué (`masked_token`) dans `/data/nexus.db` (`mcp_config`). Ce jeton **ne permet jamais** de se connecter à l'interface WebUI ni de lire vos clés API OVHcloud (`appKey`, `appSecret`, `consumerKey`).
- **Protection anti-brute-force** : Limitée à `5 tentatives échouées / 5 minutes` par adresse IP (`429 Too Many Requests`).

### 2. Bouclier de Confidentialité Bidirectionnel (`Mode Équilibré`)

Les modèles IA ne reçoivent jamais les noms d'hôtes réels OVHcloud (`ns3198452.ip-...`), les adresses IP complètes, les identifiants NIC (`xx12345-ovh`), ni les liens de paiement contenant `orderPassword` :

| Type de Ressource | Valeur OVHcloud Réelle (Masquée à l'IA) | Alias IA | Indice Contextuel Sûr (`Mode Équilibré`) |
| :--- | :--- | :---: | :--- |
| **Compte OVH** | `ovh-eu` / `xx12345-ovh` / `admin@example.com` | `acct-01` | Alias personnalisé (`EU-Main`) + `ovh-eu (IE)` |
| **Serveur Dédié** | `ns3198452.ip-51-210-88.eu` | `bm-01` | Nom personnalisé sûr + `KS-A @ GRA (*.*.*.42)` |
| **Instance VPS** | `vps-98a7bc12.vps.ovh.net` | `vps-01` | Nom personnalisé sûr + `VPS-1 @ GRA (*.*.*.88)` |
| **IP Publique** | `51.210.88.42` / `2001:41d0:...:1` | `ip-01` | Dernier octet uniquement (`*.*.*.42` / `*::0001`) |
| **Commande** | `238910421` + `orderUrl?orderPassword=...` | `ord-01` | Prix TTC et suivi en 4 étapes (**URL de paiement supprimée**) |

Lorsque l'IA appelle un outil avec un alias (par ex. `baremetal_get(server="bm-01")`), la passerelle résout automatiquement l'alias depuis `/data/nexus.db` (`mcp_aliases`) vers l'identifiant OVHcloud réel.

### 3. Sandbox de Permissions à 4 Niveaux (`L1` – `L4`)

| Niveau | Clé Scope | Défaut | Capacités & Lignes Rouges |
| :--- | :---: | :---: | :--- |
| **L1 Lecture Seule** | `read` | **ACTIF** | **11 Outils** : Inventaire des comptes, serveurs dédiés, VPS, pare-feu IP, échéances d'expiration, suivi des commandes, disponibilité temps réel (`catalog_availability` avec pagination `offset`, `nextOffset`, `listingState`, `generation` et filtre `presale_only`) et chronologie des changements SKU (`catalog_sku_changes`). |
| **L2 Commande Non Payée** | `procurement` | **INACTIF** | **5 Outils** : Devis et création de commande non payée en 2 étapes (`order_quote` → `order_create_unpaid`), gestion de la file d'attente (`queue_add`, `queue_manage`) et alertes de stock. **`auto_pay=false` et `quantity=1` sont verrouillés en dur.** |
| **L3 Opérations Réversibles** | `operations` | **INACTIF** | **4 Outils** : Redémarrage matériel (`baremetal_reboot`, `vps_reboot`), réinitialisation IPMI (`baremetal_ipmi_reset`) et règles du pare-feu IP Edge / Anti-DDoS (`baremetal_firewall_update`). Nécessite le 2FA activé. |
| **L4 Destructif & Financier** | *Bloqué en dur* | **VERROUILLÉ** | **0 Outil (Bloqué définitivement dans le code)** : Paiement automatique (`autoPay`), réinstallation d'OS, résiliation de service, URL KVM brutes et modification des clés SSH/API ne sont jamais exposés. |

---

## Configuration des Clients IA

Dans **WebUI → Passerelle IA MCP (`/mcp`)**, le générateur de configuration propose deux modes de copie : **Fichier Complet** (`Full Config File`) pour un fichier `mcp.json` / `mcp_config.json` vierge, ou **Entrée Serveur Uniquement** (`Server Entry Only`, copiant uniquement l'objet `"ovh-nexus": { ... }` avec rappel d'ajouter une virgule `,` après l'entrée précédente lors de l'insertion dans un bloc `mcpServers` existant).

### Claude Code (CLI)

```bash
claude mcp add --transport http ovh-nexus "http://localhost:20000/api/mcp" \
  --header "Authorization: Bearer nxs_mcp_VOTRE_JETON"
```

### Claude Desktop / Cursor (`mcp.json`)

```json
{
  "mcpServers": {
    "ovh-nexus": {
      "url": "http://localhost:20000/api/mcp",
      "headers": {
        "Authorization": "Bearer nxs_mcp_VOTRE_JETON"
      }
    }
  }
}
```

### Antigravity / Windsurf (`mcp_config.json`)

```json
{
  "mcpServers": {
    "ovh-nexus": {
      "serverUrl": "http://localhost:20000/api/mcp",
      "headers": {
        "Authorization": "Bearer nxs_mcp_VOTRE_JETON"
      }
    }
  }
}
```
