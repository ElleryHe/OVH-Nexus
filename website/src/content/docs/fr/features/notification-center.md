---
title: Centre de Notifications Multi-Canaux
description: Configurez des canaux Telegram Bot et Email SMTP avec abonnement granulaire à 7 événements.
---

Le **Centre de Notifications** (`/notifications`, propulsé par `backend/notifications/`) fournit un répartiteur multi-canaux modulaire prenant en charge simultanément plusieurs **Bots Telegram** et comptes **Email SMTP**.

## Les 7 Types d'Événements Pris en Charge

Chaque canal de notification peut s'abonner indépendamment à n'importe quelle combinaison des 7 événements système :

| Clé d'Événement | Activé par Défaut | Requiert Clé API OVH | Condition de Déclenchement |
| :--- | :---: | :---: | :--- |
| `stock.available` | Oui | Non | Une configuration serveur ou VPS surveillée passe de rupture de stock à disponible. |
| `stock.unavailable` | Non | Non | Une configuration surveillée auparavant disponible tombe en rupture de stock. |
| `sku.changed` | Oui | Non | `SkuWatcher` détecte un nouveau SKU mis en baie, un retrait confirmé ou une modification de sous-configuration. |
| `order.success` | Oui | Oui | La file d'achat ou le sniper génère avec succès une commande OVHcloud (inclut le lien de paiement). |
| `order.failed` | Oui | Oui | Une tentative d'achat automatisé échoue lors de la création ou de la validation du panier. |
| `order.delivered` | Oui | Oui | Une commande suivie dans `/me/order` termine les 4 étapes de livraison (`INSTALLATION` terminée). |
| `service.expiring` | Non | Oui | Un serveur dédié ou VPS arrive à moins de `expiryDays` jours (défaut `7` jours) de son expiration. |

## Canal Type 1 : Bot Telegram

1. Contactez [@BotFather](https://t.me/BotFather) sur Telegram et envoyez `/newbot` pour obtenir un **Bot Token** (`123456789:ABCdef...`).
2. Récupérez votre **Chat ID** (identifiant personnel, groupe `-100...` ou canal).
3. Dans **Centre de Notifications → Ajouter un Canal**, choisissez **Telegram Bot**, saisissez un nom, collez votre **Bot Token** et votre **Chat ID**, cochez les événements souhaités, puis cliquez sur **Tester** et **Enregistrer**.

:::note[Sécurité du Webhook Telegram]
Si vous configurez un webhook Telegram (`/api/telegram/webhook`), OVH Nexus vérifie chaque requête entrante en temps constant via l'en-tête `X-Telegram-Bot-Api-Secret-Token` (`data/webhook_secret`) et n'exécute les commandes que pour le `chatId` autorisé.
:::

## Canal Type 2 : Email SMTP

OVH Nexus intègre un `SmtpNotifier` (`backend/notifications/smtp.py`) qui envoie des e-mails multipart contenant à la fois une **version texte brut** et un **modèle HTML responsive aux couleurs OVHcloud Blue** (`MIME Base64/UTF-8`).

| Champ | Exemple | Description |
| :--- | :--- | :--- |
| **Hôte SMTP (`host`)** | `smtp.gmail.com` / `mail.domaine.fr` | Nom d'hôte du serveur SMTP sortant. |
| **Port SMTP (`port`)** | `465` (SSL/TLS) ou `587` (STARTTLS) | Port du serveur SMTP. |
| **Chiffrement (`encryption`)** | `ssl` (`465`), `starttls` (`587`) ou `none` (`25`) | Mode de sécurité de transport (`ssl` utilise `smtplib.SMTP_SSL` avec les certificats CA système). |
| **Nom d'utilisateur (`username`)** | `alerts@example.com` | Identifiant de connexion SMTP (optionnel pour un relais local sans authentification). |
| **Mot de passe (`password`)** | `mot-de-passe-application` | Mot de passe SMTP (masqué sous la forme `****` après enregistrement). |
| **Adresse d'expéditeur (`fromEmail`)** | `OVH Nexus <alerts@example.com>` | Adresse affichée dans l'en-tête `From`. |
| **Destinataires (`toEmails`)** | `ops@example.com, admin@example.com` | Liste d'adresses e-mail destinataires séparées par des virgules. |

## Masquage des Secrets (`****`)

Lorsque l'interface web récupère la configuration des notifications (`GET /api/notifications/config`), `NotificationHub` masque systématiquement tous les champs sensibles (`token` pour Telegram, `password` pour SMTP) sous la forme `abcd****wxyz`. Lorsque vous modifiez un canal et cliquez sur **Enregistrer** ou **Tester**, le backend conserve automatiquement le secret stocké sur disque sans que vous ayez à le ressaisir.
