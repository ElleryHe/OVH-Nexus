---
title: Clés API OVHcloud
description: Comment générer vos App Key, App Secret et Consumer Key OVHcloud pour les régions Europe, Canada et US.
---

Bien que les fonctionnalités publiques (**Matrice de Disponibilité en Direct** et **Veille Publique des SKU**) fonctionnent sans aucun identifiant OVHcloud, la gestion de votre flotte achetée, le suivi des commandes et l'achat automatisé nécessitent un jeu de clés API OVHcloud (`App Key`, `App Secret` et `Consumer Key`).

## 1. Ouvrir le Générateur de Token Régional

Choisissez la page de création de token correspondant à la région de votre compte OVHcloud :

| Région | Code Endpoint | URL de Création de Token |
| :--- | :--- | :--- |
| **Europe** | `ovh-eu` | `https://eu.api.ovh.com/createToken/` |
| **Canada / Asie-Pacifique** | `ovh-ca` | `https://ca.api.ovh.com/createToken/` |
| **États-Unis** | `ovh-us` | `https://api.us.ovhcloud.com/createToken/` |

## 2. Remplir le Formulaire

1. **Account ID / NIC-handle ou Email** & **Password** : Connectez-vous avec votre compte OVHcloud (et validez votre 2FA).
2. **Script Name** : `OVH Nexus`
3. **Script Description** : `Self-hosted OVH Nexus control and procurement console`
4. **Validity** : Choisissez `Unlimited` (recommandé pour la surveillance continue et le suivi des renouvellements) ou `30 days`.
5. **Rights** : Ajoutez les méthodes HTTP et chemins requis ci-dessous.

## 3. Règles de Permissions Recommandées

### Option A : Fonctionnalités Complètes (Le plus simple)

Si vous utilisez OVH Nexus à la fois pour la commande automatisée et l'administration complète de vos serveurs dédiés et VPS :

| Méthode | Chemin (Path) |
| :--- | :--- |
| `GET` | `/*` |
| `POST` | `/*` |
| `PUT` | `/*` |
| `DELETE` | `/*` |

### Option B : Permissions Restreintes au Moindre Privilège

Si vous souhaitez limiter le token uniquement aux espaces de noms API utilisés par OVH Nexus :

| Groupe de Fonctionnalités | Méthodes | Modèles de Chemin |
| :--- | :--- | :--- |
| **Compte, Commandes & Clés SSH** | `GET`, `POST`, `DELETE` | `/me`, `/me/*` |
| **Panier & Commande Automatisée** | `GET`, `POST`, `PUT`, `DELETE` | `/order/*` |
| **Contrôle des Serveurs Dédiés & IPMI** | `GET`, `POST`, `PUT` | `/dedicated/server`, `/dedicated/server/*` |
| **Gabarits d'Installation OS** | `GET` | `/dedicated/installationTemplate`, `/dedicated/installationTemplate/*` |
| **Gestion de la Flotte VPS** | `GET`, `POST` | `/vps`, `/vps/*` |
| **Pare-feu IP Edge & Anti-DDoS** | `GET`, `POST`, `PUT`, `DELETE` | `/ip`, `/ip/*` |

## 4. Enregistrer les Clés dans OVH Nexus

1. Connectez-vous à l'interface web OVH Nexus.
2. Ouvrez la page **Comptes API** (ou **Paramètres**).
3. Saisissez votre **App Key**, **App Secret** et **Consumer Key**, sélectionnez l'**Endpoint** (`ovh-eu`, `ovh-ca` ou `ovh-us`), et vérifiez la **Zone / Filiale (`zone`)** (ex. `FR`, `IE`, `DE`, `CA`, `SG`, `US`).
4. Cliquez sur **Vérifier / Enregistrer**. OVH Nexus interrogera `/me` pour valider la connexion et afficher votre `nichandle` ainsi que votre filiale de facturation.
