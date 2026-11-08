import React, { useId } from 'react';

interface NexusLogoProps {
  size?: number;
  className?: string;
}

/**
 * OVH Nexus Brand Mark
 * Combines the OVHcloud Royal Blue squircle with an isometric hexagonal cloud
 * topology, 3-tier baremetal server bus lines, a bold "N" (Nexus) backbone,
 * and an active sky-cyan radar/availability node.
 */
export const NexusLogo: React.FC<NexusLogoProps> = ({ size = 36, className = '' }) => {
  const uid = useId().replace(/:/g, '');
  const bgId = `nexus-bg-${uid}`;
  const glowId = `nexus-glow-${uid}`;
  const bridgeId = `nexus-bridge-${uid}`;

  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="0 0 64 64"
      fill="none"
      className={`shrink-0 select-none ${className}`}
      aria-label="OVH Nexus Logo"
    >
      <defs>
        <linearGradient id={bgId} x1="6" y1="4" x2="58" y2="60" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#0B65FF" />
          <stop offset="50%" stopColor="#0050D7" />
          <stop offset="100%" stopColor="#00297A" />
        </linearGradient>

        <radialGradient id={glowId} cx="20" cy="14" r="36" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#60A5FA" stopOpacity="0.35" />
          <stop offset="100%" stopColor="#0050D7" stopOpacity="0" />
        </radialGradient>

        <linearGradient id={bridgeId} x1="20" y1="20" x2="44" y2="44" gradientUnits="userSpaceOnUse">
          <stop offset="0%" stopColor="#FFFFFF" />
          <stop offset="100%" stopColor="#93C5FD" />
        </linearGradient>
      </defs>

      {/* Squircle Base */}
      <rect x="2" y="2" width="60" height="60" rx="15" fill={`url(#${bgId})`} />
      <rect x="2" y="2" width="60" height="60" rx="15" fill={`url(#${glowId})`} />
      <rect
        x="2.75"
        y="2.75"
        width="58.5"
        height="58.5"
        rx="14.25"
        stroke="#FFFFFF"
        strokeOpacity="0.18"
        strokeWidth="1.5"
      />

      {/* Outer Isometric Hexagonal Cloud Topology */}
      <polygon
        points="32,10 51,21 51,43 32,54 13,43 13,21"
        stroke="#93C5FD"
        strokeOpacity="0.28"
        strokeWidth="1.75"
        strokeLinejoin="round"
      />

      {/* Horizontal Server Rack Bus Lines */}
      <line x1="21" y1="22" x2="43" y2="22" stroke="#60A5FA" strokeOpacity="0.35" strokeWidth="2" strokeLinecap="round" />
      <line
        x1="21"
        y1="32"
        x2="43"
        y2="32"
        stroke="#60A5FA"
        strokeOpacity="0.45"
        strokeWidth="2"
        strokeLinecap="round"
        strokeDasharray="3 3"
      />
      <line x1="21" y1="42" x2="43" y2="42" stroke="#60A5FA" strokeOpacity="0.35" strokeWidth="2" strokeLinecap="round" />

      {/* Core Nexus "N" + Dual Server Pillar Architecture */}
      <path
        d="M21 43V21L43 43V21"
        stroke={`url(#${bridgeId})`}
        strokeWidth="4.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />

      {/* Top & Bottom Apex Uplink Nodes */}
      <line x1="32" y1="10" x2="21" y2="21" stroke="#93C5FD" strokeOpacity="0.55" strokeWidth="1.75" strokeLinecap="round" />
      <line x1="32" y1="54" x2="43" y2="43" stroke="#93C5FD" strokeOpacity="0.55" strokeWidth="1.75" strokeLinecap="round" />
      <circle cx="32" cy="10" r="2.5" fill="#DBEAFE" />
      <circle cx="32" cy="54" r="2.5" fill="#DBEAFE" />

      {/* Primary Nexus Corner Nodes */}
      <circle cx="21" cy="21" r="3.5" fill="#FFFFFF" />
      <circle cx="21" cy="43" r="3.5" fill="#FFFFFF" />
      <circle cx="43" cy="43" r="3.5" fill="#FFFFFF" />

      {/* Active Radar / Availability Pulse Node (Top-Right) */}
      <circle cx="43" cy="21" r="6" fill="#38BDF8" fillOpacity="0.3" />
      <circle cx="43" cy="21" r="3.75" fill="#38BDF8" stroke="#FFFFFF" strokeWidth="1.5" />
    </svg>
  );
};

export default NexusLogo;
