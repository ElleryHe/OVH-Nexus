---
title: Contrôle des Serveurs Dédiés
description: Gérez vos serveurs dédiés OVHcloud, consoles IPMI/KVM, réinstallation OS, clés SSH et pare-feu IP Edge.
---

Le module **Contrôle des Serveurs Dédiés** (`/server-control`) offre un pilotage opérationnel complet sur l'ensemble des serveurs bare-metal (`Kimsufi`, `So you Start`, `Rise`, `Advance`, `Scale`, `High Grade`) rattachés à votre compte OVHcloud actif.

## Inventaire Matériel & Télémétrie MRTG

La sélection d'un serveur affiche :

- **Spécifications Matérielles** : Modèle commercial, processeur, capacité RAM, agencement des disques, code datacenter et numéro de baie (Rack ID).
- **Réseau & Bande Passante** : Adresses IPv4/IPv6 publiques, reverse DNS et **graphiques de trafic MRTG en temps réel** (`/dedicated/server/{serviceName}/statistics`) pour le trafic entrant et sortant (`hourly`, `daily`, `weekly`, `monthly`, `yearly`).
- **Alimentation & Mode Rescue** : Redémarrage matériel (`/reboot`) ou passage en netboot rescue (`rescue64-pro`).

## Accès Distant IPMI / KVM

Pour les serveurs équipés d'un contrôleur BMC/IPMI :

- **Serial over LAN (SOL) & xterm.js** : Ouvrez une session terminal interactive directement dans le navigateur ou récupérez les identifiants SSH SOL.
- **KVM sur IP / Console HTML5** : Demandez un accès KVM temporaire via `/dedicated/server/{serviceName}/features/ipmi/access` (autorisation automatique de votre adresse IP publique) et suivez l'avancement de la tâche IPMI en direct.

## Réinstallation OS Automatisée & Injection de Clés SSH

OVH Nexus simplifie le provisionnement système des serveurs dédiés (`/dedicated/server/{serviceName}/install/start`) :

1. **Catalogue de Gabarits OS** : Liste les images officielles OVHcloud compatibles avec le serveur (Debian, Ubuntu, Rocky Linux, AlmaLinux, Proxmox VE, Windows Server, etc.).
2. **Partitionnement Personnalisé, RAID & ZFS** :
   - Configurez le niveau de RAID matériel ou logiciel (`RAID 0`, `RAID 1`, `RAID 5`, `RAID 10` ou `JBOD`).
   - Génération en un clic d'un profil de système de fichiers racine **Proxmox VE ZFS**.
   - Personnalisation de la taille des partitions et du nom d'hôte (`hostname`).
3. **Gestion des Clés Publiques SSH (`/me/sshKey`)** :
   - Consultez, ajoutez ou supprimez les clés publiques SSH enregistrées sur votre compte OVHcloud directement depuis la fenêtre **Clés SSH du Compte**.
   - Sélectionnez un `sshKeyName` lors du lancement de l'installation afin qu'OVHcloud injecte automatiquement votre clé publique dans `authorized_keys`.

## Pare-feu IP Edge & Mitigation Anti-DDoS

Pilotez le pare-feu réseau Edge d'OVHcloud (`/ip/{ip}/firewall`) et la protection Anti-DDoS VAC sans quitter la console :

- **Activation du Pare-feu Edge** : Activez ou désactivez le pare-feu matériel sur chaque adresse IPv4 associée au serveur.
- **Règles de Filtrage (Séquence `0–19`)** : Ajoutez ou supprimez des règles `permit` ou `deny` par protocole (`tcp`, `udp`, `icmp`, `ipv4`), CIDR source, port de destination et drapeaux TCP (`syn`, `established`).
- **Mitigation Anti-DDoS Permanente** : Consultez l'état de mitigation (`auto` vs `permanent`) et activez la mitigation VAC permanente (`/ip/{ip}/mitigation`) pour vos services sensibles à la latence.
