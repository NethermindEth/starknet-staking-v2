import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'Starknet Staking v2',
  tagline: 'Validator software for Starknet staking v2',
  favicon: 'img/favicon.ico',

  future: {
    v4: true,
  },

  url: 'https://nethermindeth.github.io',
  baseUrl: '/starknet-staking-v2/',

  organizationName: 'NethermindEth',
  projectName: 'starknet-staking-v2',

  onBrokenLinks: 'throw',
  onBrokenMarkdownLinks: 'warn',

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  presets: [
    [
      'classic',
      {
        docs: {
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    navbar: {
      title: 'Starknet Staking v2',
      logo: {
        alt: 'Starknet Logo',
        src: 'img/logo.svg',
        href: '/',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'tutorialSidebar',
          position: 'left',
        },
        {
          href: 'https://github.com/NethermindEth/starknet-staking-v2',
          label: 'GitHub',
          position: 'right',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {
              label: 'Getting Started',
              to: '/',
            },
            {
              label: 'Becoming a Validator',
              to: '/becoming-validator',
            },
            {
              label: 'Installation',
              to: '/installation',
            },
            {
              label: 'Configuration',
              to: '/configuration',
            },
          ],
        },
        {
          title: 'Community',
          items: [
            {
              label: 'Telegram',
              href: 'https://t.me/StarknetJuno',
            },
            {
              label: 'Discord',
              href: 'https://discord.com/invite/TcHbSZ9ATd',
            },
            {
              label: 'Twitter',
              href: 'https://x.com/NethermindStark',
            },
          ],
        },
        {
          title: 'More',
          items: [
            {
              label: 'GitHub',
              href: 'https://github.com/NethermindEth/starknet-staking-v2',
            },
            {
              label: 'Juno Client',
              href: 'https://github.com/NethermindEth/juno',
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} Nethermind. Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
      additionalLanguages: ['bash', 'json', 'yaml', 'docker'],
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
