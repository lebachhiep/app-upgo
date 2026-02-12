import { theme, type ThemeConfig } from 'antd'

export const darkTheme: ThemeConfig = {
  algorithm: theme.darkAlgorithm,
  token: {
    colorPrimary: '#22edeb',
    colorBgBase: '#142334',
    colorBgContainer: '#1E2D3E',
    colorBgElevated: '#24374C',
    colorBgLayout: '#142334',
    borderRadius: 8,
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif",
    fontSize: 13,
    colorBorder: '#1E344E',
    colorBorderSecondary: '#1A2F44',
    colorText: '#e0e0f0',
    colorTextSecondary: '#C5CBD3',
  },
  components: {
    Layout: {
      siderBg: '#1E2D3E',
      bodyBg: '#142334',
    },
    Menu: {
      darkItemBg: 'transparent',
      darkSubMenuItemBg: 'transparent',
      darkItemSelectedBg: 'rgba(34, 237, 235, 0.1)',
      darkItemSelectedColor: '#22edeb',
      darkItemHoverBg: 'rgba(45, 79, 118, 0.4)',
      itemHeight: 40,
      iconSize: 16,
      itemMarginInline: 8,
      itemBorderRadius: 8,
    },
    Card: {
      colorBgContainer: '#24374C',
      colorBorderSecondary: '#1E344E',
    },
    Input: {
      colorBgContainer: '#111D2D',
      colorBorder: '#1E344E',
      activeBorderColor: '#22edeb',
    },
    Select: {
      colorBgContainer: '#111D2D',
      colorBorder: '#1E344E',
    },
    Switch: {
      colorPrimary: '#22edeb',
    },
    Button: {
      borderRadius: 8,
      primaryColor: '#142334',
    },
    Modal: {
      contentBg: '#24374C',
      headerBg: '#24374C',
      titleColor: '#e0e0f0',
    },
    Tag: {
      borderRadiusSM: 4,
    },
    Descriptions: {
      colorTextSecondary: '#8B97A7',
      colorText: '#e0e0f0',
      colorSplit: '#1E344E',
    },
  },
}
