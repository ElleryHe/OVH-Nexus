---
title: Centre de Notifications Multi-Canaux
description: Configurez des canaux sortants Telegram Bot et Email SMTP avec modèles trilingues et abonnement à 8 événements.
---

Le **Centre de Notifications** (`/notifications`, propulsé par `internal/notify`) fournit un répartiteur multi-canaux purement **sortant** prenant en charge simultanément plusieurs **Bots Telegram** et comptes **Email SMTP**.

## Modèles Trilingues & Fuseau Horaire

- **Langue de Notification (`locale`)** : Sélectionnez **`en`** (English, défaut), **`fr`** (Français) ou **`zh-CN`** (简体中文) dans le **Centre de Notifications** pour recevoir les alertes Telegram et Email dans votre langue, indépendamment de la langue de l'interface web.
- **Fuseau Horaire (`TZ`)** : Les horodatages des notifications utilisent la variable `TZ` du conteneur (par défaut `UTC`, ex. `Europe/Paris` ou `Asia/Shanghai`).

## Les 8 Types d'Événements Pris en Charge

Chaque canal de notification peut s'abonner indépendamment à n'importe quelle combinaison des 8 événements système :

| Clé d'Événement | Activé par Défaut | Requiert Clé API OVH | Condition de Déclenchement |
| :--- | :---: | :---: | :--- |
| `stock.available` | Oui | Non | Une configuration serveur ou VPS surveillée passe de rupture de stock à disponible. |
| `stock.unavailable` | Non | Non | Une configuration surveillée auparavant disponible tombe en rupture de stock. |
| `sku.changed` | Oui | Non | La Veille SKU Publique détecte un nouveau SKU mis en baie, un retrait confirmé ou une modification de sous-configuration. |
| `order.success` | Oui | Oui | La file d'achat ou commande rapide génère avec succès une commande OVHcloud (inclut le lien de paiement). |
| `order.failed` | Oui | Oui | Une tentative d'achat automatisé échoue lors de la création ou de la validation du panier. |
| `order.delivered` | Oui | Oui | Une commande suivie dans `/me/order` termine les 4 étapes de livraison (`INSTALLATION` terminée). |
| `service.expiring` | Non | Oui | Un serveur dédié ou VPS arrive à moins de `expiryDays` jours (défaut `7` jours) de son expiration. |
| `mcp.action` | Oui | Non | Un client IA exécute une action L2 (commande non payée / file) ou L3 via `/api/mcp`. |

## Canal Type 1 : Bot Telegram Sortant

1. Contactez [@BotFather](https://t.me/BotFather) sur Telegram et envoyez `/newbot` pour obtenir un **Bot Token** (`123456789:ABCdef...`).
2. Récupérez votre **Chat ID** (identifiant personnel, groupe `-100...` ou canal).
3. Dans **Centre de Notifications → Ajouter un Canal**, choisissez **Telegram Bot**, saisissez un nom, collez votre **Bot Token** et votre **Chat ID**, cochez les événements souhaités, puis cliquez sur **Tester** et **Enregistrer**.

:::note[Notifications Sortantes Uniquement & URL du Panneau]
L'intégration Telegram d'OVH Nexus est exclusivement **sortante** (`https://api.telegram.org`) et ne nécessite d'exposer aucun point de terminaison de rappel entrant. Vous pouvez renseigner l'**URL du panneau** dans le Centre de Notifications afin d'inclure un lien direct vers votre console dans les alertes.
:::

## Canal Type 2 : Email SMTP

OVH Nexus intègre un moteur SMTP (`internal/notify`) qui envoie des e-mails multipart contenant à la fois une **version texte brut** et un **modèle HTML responsive aux couleurs OVHcloud Blue** (`UTF-8`).

| Champ | Exemple | Description |
| :--- | :--- | :--- |
| **Hôte SMTP (`host`)** | `smtp.gmail.com` / `mail.domaine.fr` | Nom d'hôte du serveur SMTP sortant. |
| **Port SMTP (`port`)** | `465` (SSL/TLS) ou `587` (STARTTLS) | Port du serveur SMTP. |
| **Chiffrement (`security`)** | `ssl` (`465`), `starttls` (`587`) ou `none` (`25`) | Mode de sécurité de transport vérifié via le magasin CA système (`ca-certificates`). |
| **Nom d'utilisateur (`username`)** | `alerts@example.com` | Identifiant de connexion SMTP (optionnel pour un relais local sans authentification). |
| **Mot de passe (`password`)** | `mot-de-passe-application` | Mot de passe SMTP (chiffré au repos via `AES-256-GCM` et masqué sous la forme `****`). |
| **Adresse d'expéditeur (`fromAddress`)** | `alerts@example.com` | Adresse d'expéditeur (`From`). |
| **Destinataires (`recipients`)** | `ops@example.com, admin@example.com` | Liste d'adresses e-mail destinataires séparées par des virgules. |

## Masquage des Secrets (`****`)

Lorsque l'interface web récupère la configuration des notifications (`GET /api/notifications`), OVH Nexus masque systématiquement tous les champs sensibles (`token` pour Telegram, `password` pour SMTP) sous la forme `abcd****wxyz`. Lorsque vous modifiez un canal et cliquez sur **Enregistrer** ou **Tester**, le serveur conserve automatiquement le secret chiffré stocké dans `/data/nexus.db` sans que vous ayez à le ressaisir.
