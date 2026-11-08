# OVH Nexus (Français)

<p align="center">
  <img src="../public/favicon.svg" width="72" height="72" alt="OVH Nexus Logo" />
</p>

<p align="center">
  <strong>Console de Contrôle &amp; d’Approvisionnement OVHcloud</strong><br/>
  Gestion unifiée de flotte Cloud, suivi de livraison en direct et approvisionnement automatisé de serveurs dédiés et VPS.
</p>

<p align="center">
  <a href="../README.md">English</a> ·
  <a href="./README.fr.md"><strong>Français</strong></a> ·
  <a href="./README.zh-CN.md">简体中文</a>
</p>

---

## Présentation

**OVH Nexus** est une console web auto-hébergée et multi-comptes conçue pour les utilisateurs d’OVHcloud (`OVH Europe`, `OVH Canada`, `OVH US`). Elle réunit dans un seul conteneur Docker **la gestion quotidienne de votre infrastructure Cloud** et **la surveillance de stock / commande automatisée**.

### Fonctionnalités Principales

1. **Contrôle des Serveurs Dédiés (`/dedicated/server`)**
   - Inventaire matériel, spécifications réseau, graphiques de trafic MRTG en temps réel et accès distant IPMI/KVM (SOL, KVM, xterm.js).
   - Réinstallation OS automatisée avec RAID matériel/logiciel, profils Proxmox ZFS et **injection directe de clés publiques SSH (`/me/sshKey`)**.
   - **Pare-feu IP Edge & Mitigation Anti-DDoS (`/ip/{ip}/firewall` & `/mitigation`)** : gestion des règles de pare-feu réseau OVH et activation de la mitigation permanente Anti-DDoS.
2. **Gestion de la Flotte VPS (`/vps`)**
   - Vue d’ensemble des VPS achetés (modèle, vCore/RAM/Disque, adresses IPv4/IPv6, état, échéance) et redémarrage en un clic (`/vps/{serviceName}/reboot`).
3. **Suivi des Commandes & Livraison en Direct (`/me/order` & `/followUp`)**
   - Liste des commandes OVHcloud récentes, liens de paiement/facture PDF et suivi en temps réel des 4 étapes de livraison (`CREATING`, `VALIDATING`, `DELIVERING`, `INSTALLATION`).
4. **Matrice d’Expiration & Renouvellement des Services (`/serviceInfos`)**
   - Compte à rebours avant expiration (`<= 7j`, `<= 30j`, `> 30j`) et mode de renouvellement (`Automatique`, `Manuel`, `Résiliation à l'échéance`) pour tous vos serveurs dédiés et VPS.
5. **Disponibilité en Temps Réel, Veille SKU & Centre de Notifications Multi-Canaux**
   - Matrice de disponibilité en direct dans tous les datacenters OVHcloud (`GRA`, `SBG`, `RBX`, `LIM`, `FRA`, `LON`, `WAW`, `BHS`, `SGP`, `SYD`) avec **noms commerciaux (`KS-A`, `RISE-1`), spécifications matérielles lisibles et tarification mensuelle exacte par configuration**.
   - **Veille des Changements SKU** : détection automatique des nouveaux modèles et retraits de serveurs sur les étagères publiques OVHcloud avec protection anti-faux-positifs.
   - **Centre de Notifications** : canaux multiples **Telegram Bot** et **Email SMTP** (`SSL/TLS`, `STARTTLS`) avec abonnement granulaire à 7 types d’événements.
6. **Interface Trilingue Intégrée (`en` / `fr` / `zh-CN`)**
   - Basculez instantanément entre **English**, **Français** et **简体中文** depuis l’interface web ou via la variable d’environnement `DEFAULT_LOCALE=fr`.

---

## Démarrage Rapide (Docker Compose)

### 1. Créer le fichier `.env`

```bash
cat > .env << 'EOF'
API_SECRET_KEY=votre-cle-secrete-robuste
DEFAULT_LOCALE=fr
COOKIE_SECURE=false
EOF
```

### 2. Lancer le conteneur

```bash
docker compose up -d
```

Accédez ensuite à `http://localhost:20000` et connectez-vous avec `API_SECRET_KEY`.

---

## Licence & Clause de non-responsabilité

- **Licence** : Distribué sous licence **GNU Affero General Public License v3.0 (AGPL-3.0)**. Voir le fichier [LICENSE](../LICENSE).
- **Avertissement** : **OVH Nexus** est un projet open-source tiers indépendant et n’est **pas affilié à, ni approuvé par OVH SAS / OVHcloud**. « OVH » et « OVHcloud » sont des marques déposées d’OVH SAS.
