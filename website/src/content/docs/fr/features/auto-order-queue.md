---
title: Approvisionnement Automatisé & File d'Achat
description: Configurez des tâches d'achat automatisé de serveurs dédiés et la surveillance par sous-configuration matérielle.
---

OVH Nexus propose deux moteurs complémentaires pour automatiser l'acquisition de serveurs dédiés lorsque des modèles très demandés (`KS-LE`, `KS-A` ou ventes flash) ne restent en stock que quelques secondes :

1. **File d'Achat (`/queue`)** : Surveille un `planCode` cible sur un ou plusieurs datacenters prioritaires avec options matérielles, mode d'engagement (`pricingMode` & `duration`) et code promo optionnel (`coupon`), en enregistrant l'origine de chaque tâche (`source: "manual" | "stock-monitor" | "mcp"`).
2. **Surveillance Multi-Cibles par Configuration (`/monitor`, `/availability`, `/servers`)** : Chaque abonnement de stock gère une ou plusieurs **cibles de configuration (`targets[]`)** via la fenêtre **Surveillance rapide (`QuickMonitorDialog`)** :
   - **Notification seule vs. Achat automatique avec configuration explicite** : Une cible en mode notification seule peut surveiller `Toute configuration` (`configKey: ""`), tandis que l'activation de **`autoOrder: true` exige obligatoirement le choix d'une configuration précise (`configKey` + `options`)** (`400 CONFIG_REQUIRED_FOR_AUTO_ORDER`). L'achat déclenché par `stock-monitor` ne se replie jamais aveuglément sur la première configuration disponible.
   - **Modes d'Engagement Multi-Périodes (`pricingMode` & `duration`)** : Prise en charge de la facturation mensuelle sans engagement (`default` / `P1M`), annuelle prépayée 12 mois (`upfront12` / `P12M`), biennale prépayée 24 mois (`upfront24` / `P24M`) et mensuelle dégressive (`degressivity12` / `degressivity24`). Le changement de période met à jour en temps réel le total par période, l'équivalent mensuel HT remisé et l'exonération des frais d'installation (`0,00 €` en `upfront12`/`upfront24`).
   - **Quantité (`quantity`) & Plafond Mensuel Équivalent HT (`maxMonthlyPrice`) par Cible** : Chaque cible d'achat automatique définit sa quantité (`quantity >= 1`, défaut `1`) et un **prix mensuel maximum HT (`maxMonthlyPrice`)** (prérempli avec l'équivalent mensuel HT remisé `monthlyEquivalentPrice` du mode choisi et modifiable ; les frais d'installation uniques sont affichés séparément et exclus du plafond mensuel).
   - **Code Promo / Coupon (`coupon`) avec Arrêt Immédiat en Cas d'Invalidité (`INVALID_COUPON`)** : Vous pouvez associer un code promo optionnel (`coupon`) à toute commande directe, tâche de file d'attente ou cible de surveillance. Si OVHcloud refuse le code lors de la préparation du panier, la commande est immédiatement interrompue (`400 INVALID_COUPON`) afin de ne jamais commander au plein tarif par erreur.
   - **Migration Sécurisée des Anciens Abonnements (`needsConfigSelection`)** : Les anciens abonnements sans configuration explicite sont migrés vers une cible « Toute configuration » en notification seule ; si `autoOrder` était activé, l'achat automatique est mis en pause (`needsConfigSelection: true`) avec une bannière d'avertissement sur `/monitor` jusqu'à sélection d'une configuration et d'un plafond de prix.

## Déroulement du Processus d'Achat Automatisé

Dès qu'une goroutine de surveillance détecte du stock disponible (`1H-low`, `1H-high`, `24H`, `72H` ou `available`) sur la configuration et le datacenter ciblés :

1. **Création du Panier** : Appelle `POST /order/cart` avec la filiale `ovhSubsidiary` de votre compte (`zone`, ex. `FR`, `IE`, `CA`, `US`) puis assigne le panier à votre compte authentifié (`POST /order/cart/{cartId}/assign`).
2. **Ajout du Serveur & du Mode d'Engagement** : Appelle `POST /order/cart/{cartId}/eco` (ou `/baremetal`) avec le `planCode`, le `pricingMode` (`default`, `upfront12`, `upfront24`, etc.), la `duration` (`P1M`, `P12M`/`P1Y`, `P24M`/`P2Y`) et la `quantity` configurée sur la cible.
3. **Configuration du Datacenter & des Options (Addons)** :
   - Définit le `datacenter` cible sur l'élément du panier.
   - Attache les codes `options` exacts (`memory`, `storage`, `system-storage`, bande passante) associés à la cible avec le `pricingMode` et la `duration` correspondants.
4. **Application & Validation Stricte du Code Promo (`coupon`)** :
   - Si un `coupon` est renseigné, appelle `POST /order/cart/{cartId}/coupons` (`{"coupon": coupon}`).
   - En cas de rejet par OVHcloud, le panier temporaire est immédiatement supprimé, un journal `WARNING` et une alerte `order.failed` sont émis, et l'opération échoue avec `400 INVALID_COUPON` sans jamais poursuivre au tarif normal.
5. **Vérification de l'Équivalent Mensuel HT en Direct (`maxMonthlyPrice`)** :
   - Consulte le devis en direct du panier (`GET /order/cart/{cartId}/summary`), divise le tarif périodique HT remisé par la durée d'engagement (`12` pour `upfront12`, `24` pour `upfront24`, `1` pour mensuel) afin d'obtenir `liveMonthlyExVAT`, puis le compare à `maxMonthlyPrice` (si `> 0`).
   - En cas de dépassement, la validation est bloquée (`PRICE_ABOVE_LIMIT`), un journal `WARNING` est enregistré et une notification `order.failed` est envoyée.
6. **Validation du Bon de Commande** : Appelle `POST /order/cart/{cartId}/checkout` avec `auto_pay=false` et `waive_retractation_period=false` par défaut (préservant le droit légal européen de rétractation de 14 jours et générant une commande non payée avec son lien de paiement, sauf opt-in explicite validé par 2FA Sudo).
7. **Envoi de la Notification** : Émet l'événement `order.success` (comprenant le numéro de commande, le récapitulatif matériel, le prix TTC et le lien direct de paiement OVHcloud) ou `order.failed` via le **Centre de Notifications**.

## Créer une Tâche ou une Cible en Un Clic

Depuis le **Catalogue Serveurs (`/servers`)** ou la **Matrice de Disponibilité en Direct (`/availability`)** :

1. Sélectionnez le modèle et la combinaison `mémoire + stockage` souhaités dans le menu déroulant de la ligne ou dans la **Fenêtre de Configuration & Commande de Serveur**.
2. Cliquez sur **Ajouter à la file** ou **Surveillance rapide** (la configuration et les datacenters sélectionnés sont automatiquement préremplis).
3. Choisissez la période de facturation (`default` mensuel, `upfront12` annuel 12 mois ou `upfront24` biennal 24 mois), saisissez éventuellement un **Code Promo (`coupon`)** et, si vous activez l'**Achat automatique**, vérifiez la configuration précise, la quantité `quantity` et le **prix mensuel maximum HT (`maxMonthlyPrice`)**.
4. Une fois la commande générée avec succès, la tâche passe automatiquement à l'état terminé et enregistre la commande dans **Historique & Commandes (`/history`)**.
